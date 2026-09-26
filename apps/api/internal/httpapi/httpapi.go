// Package httpapi exposes the monitor management HTTP API over the
// stdlib net/http ServeMux (Go 1.22+ method/path patterns).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/monitor"
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// Store is the subset of the persistence layer the HTTP API needs. It is
// satisfied by *store.Store; tests use an in-memory fake so they don't
// depend on Postgres.
type Store interface {
	CreateMonitor(ctx context.Context, name, description, url string) (db.Monitor, error)
	ListMonitors(ctx context.Context) ([]db.Monitor, error)
	GetMonitor(ctx context.Context, id uuid.UUID) (db.Monitor, error)
	DeleteMonitor(ctx context.Context, id uuid.UUID) error
	ListDailyStatsSince(ctx context.Context, id uuid.UUID, since time.Time) ([]db.DailyStat, error)
}

// Scheduler is the subset of the scheduler the HTTP API needs to trigger
// and observe manual checks. It is satisfied by *scheduler.Scheduler.
type Scheduler interface {
	RunManualCheck(ctx context.Context, id uuid.UUID) (db.Monitor, error)
	IsChecking(id uuid.UUID) bool
}

const defaultHistoryDays = 90

// api holds the handlers' dependencies.
type api struct {
	store     Store
	scheduler Scheduler
	bgCtx     context.Context // base context for fire-and-forget background checks
	interval  time.Duration
	loc       *time.Location
}

// New builds the API routes on a fresh http.ServeMux. bgCtx is used as the
// base context for checks triggered in the background (POST
// /api/monitors/check); it should be canceled on shutdown so those checks
// abort cleanly.
func New(s Store, sched Scheduler, bgCtx context.Context, interval time.Duration, loc *time.Location) *http.ServeMux {
	a := &api{store: s, scheduler: sched, bgCtx: bgCtx, interval: interval, loc: loc}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.HandleFunc("GET /api/monitors", a.handleListMonitors)
	mux.HandleFunc("POST /api/monitors", a.handleCreateMonitor)
	mux.HandleFunc("POST /api/monitors/check", a.handleCheckAllMonitors)
	mux.HandleFunc("DELETE /api/monitors/{id}", a.handleDeleteMonitor)
	mux.HandleFunc("POST /api/monitors/{id}/check", a.handleCheckMonitor)
	// Catches any /api/* request that didn't match a route above, so it
	// never falls through to the SPA handler mounted at "/" and returns
	// index.html for a typo'd or unsupported API path.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Recurso no encontrado")
	})

	return mux
}

func (a *api) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type monitorsResponse struct {
	IntervalSeconds int               `json:"intervalSeconds"`
	Days            int               `json:"days"`
	Monitors        []monitorResponse `json:"monitors"`
}

type monitorResponse struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	URL           string       `json:"url"`
	Status        string       `json:"status"`
	Checking      bool         `json:"checking"`
	LastCheck     *lastCheck   `json:"lastCheck"`
	UptimePercent *float64     `json:"uptimePercent"`
	History       []historyDay `json:"history"`
}

type lastCheck struct {
	At         string `json:"at"`
	HTTPStatus int    `json:"httpStatus"`
	LatencyMs  int    `json:"latencyMs"`
	ErrorKind  string `json:"errorKind"`
}

type historyDay struct {
	Date            string  `json:"date"`
	State           string  `json:"state"`
	DownMinutes     float64 `json:"downMinutes"`
	DegradedMinutes float64 `json:"degradedMinutes"`
	Checks          int     `json:"checks"`
	Fails           int     `json:"fails"`
}

func (a *api) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	days := defaultHistoryDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || (n != 30 && n != 60 && n != 90) {
			writeError(w, http.StatusBadRequest, "El parámetro days debe ser 30, 60 o 90")
			return
		}
		days = n
	}

	monitors, err := a.store.ListMonitors(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	resp := monitorsResponse{
		IntervalSeconds: int(a.interval.Seconds()),
		Days:            days,
		Monitors:        make([]monitorResponse, 0, len(monitors)),
	}
	for _, m := range monitors {
		mr, err := a.buildMonitorResponse(r.Context(), m, days)
		if err != nil {
			writeInternalError(w, err)
			return
		}
		resp.Monitors = append(resp.Monitors, mr)
	}

	writeJSON(w, http.StatusOK, resp)
}

type createMonitorRequest struct {
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func (a *api) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var req createMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "El cuerpo de la solicitud no es JSON válido")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, http.StatusBadRequest, "Ingresa un nombre para la API")
		return
	}
	if !isValidMonitorURL(req.URL) {
		writeError(w, http.StatusBadRequest, "Ingresa una URL válida (http o https)")
		return
	}

	created, err := a.store.CreateMonitor(r.Context(), name, req.Description, req.URL)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	// Run the first check synchronously so the 201 response already
	// reflects the monitor's initial state instead of "pending".
	if _, err := a.scheduler.RunManualCheck(r.Context(), created.ID); err != nil {
		slog.Warn("httpapi: initial check failed", "monitor_id", created.ID, "error", err)
	}

	m, err := a.store.GetMonitor(r.Context(), created.ID)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	mr, err := a.buildMonitorResponse(r.Context(), m, defaultHistoryDays)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mr)
}

func (a *api) handleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMonitorID(w, r)
	if !ok {
		return
	}

	if err := a.store.DeleteMonitor(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "El monitor no existe")
			return
		}
		writeInternalError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *api) handleCheckMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMonitorID(w, r)
	if !ok {
		return
	}

	m, err := a.scheduler.RunManualCheck(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "El monitor no existe")
			return
		}
		writeInternalError(w, err)
		return
	}

	mr, err := a.buildMonitorResponse(r.Context(), m, defaultHistoryDays)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, mr)
}

func (a *api) handleCheckAllMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := a.store.ListMonitors(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	for _, m := range monitors {
		id := m.ID
		go func() {
			if _, err := a.scheduler.RunManualCheck(a.bgCtx, id); err != nil {
				slog.Warn("httpapi: background check failed", "monitor_id", id, "error", err)
			}
		}()
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

// buildMonitorResponse assembles the public JSON shape for a monitor,
// including its uptime percentage and per-day history for the last days
// days (oldest first), filling in gaps with "none" days.
func (a *api) buildMonitorResponse(ctx context.Context, m db.Monitor, days int) (monitorResponse, error) {
	resp := monitorResponse{
		ID:          m.ID.String(),
		Name:        m.Name,
		Description: m.Description,
		URL:         m.Url,
		Status:      m.Status,
		Checking:    a.scheduler.IsChecking(m.ID),
	}

	if m.LastCheckedAt.Valid {
		resp.LastCheck = &lastCheck{
			At:         m.LastCheckedAt.Time.Format(time.RFC3339),
			HTTPStatus: int(m.LastHttpStatus.Int32),
			LatencyMs:  int(m.LastLatencyMs.Int32),
			ErrorKind:  m.LastErrorKind.String,
		}
	}

	today := time.Now().In(a.loc)
	since := today.AddDate(0, 0, -(days - 1))

	stats, err := a.store.ListDailyStatsSince(ctx, m.ID, since)
	if err != nil {
		return monitorResponse{}, err
	}
	byDate := make(map[string]db.DailyStat, len(stats))
	for _, s := range stats {
		// Day is a date-only value stored/read as midnight UTC; format it
		// directly without reinterpreting in another location, or the
		// calendar date would shift by a day.
		byDate[s.Day.Time.Format("2006-01-02")] = s
	}

	var totalDown, totalDegraded, daysWithData int
	history := make([]historyDay, 0, days)
	for i := days - 1; i >= 0; i-- {
		dateStr := today.AddDate(0, 0, -i).Format("2006-01-02")
		s, ok := byDate[dateStr]
		if !ok {
			history = append(history, historyDay{Date: dateStr, State: string(monitor.DayNone)})
			continue
		}

		state := monitor.DayStatus(int(s.DownSeconds), int(s.DegradedSeconds), int(s.Checks))
		history = append(history, historyDay{
			Date:            dateStr,
			State:           string(state),
			DownMinutes:     float64(s.DownSeconds) / 60,
			DegradedMinutes: float64(s.DegradedSeconds) / 60,
			Checks:          int(s.Checks),
			Fails:           int(s.Fails),
		})
		totalDown += int(s.DownSeconds)
		totalDegraded += int(s.DegradedSeconds)
		daysWithData++
	}

	resp.UptimePercent = monitor.Uptime(totalDown, totalDegraded, daysWithData)
	resp.History = history

	return resp, nil
}

func parseMonitorID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "El id no es un UUID válido")
		return uuid.UUID{}, false
	}
	return id, true
}

func isValidMonitorURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	if err != nil {
		return false
	}
	return (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Warn("httpapi: internal error", "error", err)
	writeError(w, http.StatusInternalServerError, "Ocurrió un error interno")
}
