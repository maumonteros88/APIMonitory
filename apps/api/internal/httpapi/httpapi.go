// Package httpapi exposes the monitor management HTTP API over the
// stdlib net/http ServeMux (Go 1.22+ method/path patterns).
package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"apimonitory/internal/events"
	"apimonitory/internal/monitor"
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// Store is the subset of the persistence layer the HTTP API needs. It is
// satisfied by *store.Store; tests use an in-memory fake so they don't
// depend on Postgres.
type Store interface {
	CreateMonitor(ctx context.Context, name, description, url string, labels store.MonitorLabels) (db.Monitor, error)
	ListMonitors(ctx context.Context) ([]db.Monitor, error)
	GetMonitor(ctx context.Context, id uuid.UUID) (db.Monitor, error)
	UpdateMonitorDetails(ctx context.Context, id uuid.UUID, name, description string, labels store.MonitorLabels) (db.Monitor, error)
	DeleteMonitor(ctx context.Context, id uuid.UUID) error
	ListDailyStatsSince(ctx context.Context, id uuid.UUID, since time.Time) ([]db.DailyStat, error)
	ListAllDailyStatsSince(ctx context.Context, since time.Time) ([]db.DailyStat, error)
	ListScheduledChecksBetween(ctx context.Context, id uuid.UUID, from, to time.Time) ([]db.ListScheduledChecksBetweenRow, error)
}

// Scheduler is the subset of the scheduler the HTTP API needs to trigger
// and observe manual checks. It is satisfied by *scheduler.Scheduler.
type Scheduler interface {
	RunManualCheck(ctx context.Context, id uuid.UUID) (db.Monitor, error)
	IsChecking(id uuid.UUID) bool
}

const defaultHistoryDays = 90

// recentHistoryDays is how many trailing days of history a "recent" list
// response or an SSE event carries, instead of the full defaultHistoryDays:
// a past day's daily_stats row never changes once its day ends (in
// APP_TIMEZONE), so only the last recentHistoryDays days can still change
// between polls/events. 2 (not 1) covers the day-rollover window: a check
// imputed just before midnight can still complete just after it, so
// yesterday's row can keep changing into the first moments of today.
const recentHistoryDays = 2

// api holds the handlers' dependencies.
type api struct {
	store        Store
	scheduler    Scheduler
	bgCtx        context.Context // base context for fire-and-forget background checks
	interval     time.Duration
	loc          *time.Location
	token        string
	hub          *events.Hub
	sseHeartbeat time.Duration
	maxGap       time.Duration
	commit       string // build identity, reported by /healthz
	build        string // unique release id, reported by /healthz
}

// New builds the API routes on a fresh http.ServeMux. bgCtx is used as the
// base context for checks triggered in the background (POST
// /api/monitors/check); it should be canceled on shutdown so those checks
// abort cleanly. token is the shared secret required (as "Authorization:
// Bearer <token>") on every /api/* route except /healthz: without it,
// anyone with network access to the port could delete monitors (cascading,
// irreversible) or create ones that get probed on a schedule. hub feeds
// GET /api/events; sseHeartbeat is how often that route sends a keep-alive
// comment to hold the connection open through an idle-timeout proxy. maxGap
// is how far apart two scheduled checks can be before handleIncidents treats
// the gap itself as ending a down/degraded run (see main.go for how it's
// computed). commit and build identify the binary (git commit, unique
// release id) and are reported by GET /healthz so a deploy can confirm
// which one is live.
func New(s Store, sched Scheduler, bgCtx context.Context, interval time.Duration, loc *time.Location, token string, hub *events.Hub, sseHeartbeat time.Duration, maxGap time.Duration, commit string, build string) *http.ServeMux {
	a := &api{
		store:        s,
		scheduler:    sched,
		bgCtx:        bgCtx,
		interval:     interval,
		loc:          loc,
		token:        token,
		hub:          hub,
		sseHeartbeat: sseHeartbeat,
		maxGap:       maxGap,
		commit:       commit,
		build:        build,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.handleHealthz)
	mux.Handle("GET /api/monitors", a.requireToken(a.handleListMonitors))
	mux.Handle("GET /api/events", a.requireToken(a.handleEvents))
	mux.Handle("POST /api/monitors", a.requireToken(a.handleCreateMonitor))
	mux.Handle("POST /api/monitors/check", a.requireToken(a.handleCheckAllMonitors))
	mux.Handle("PATCH /api/monitors/{id}", a.requireToken(a.handleUpdateMonitor))
	mux.Handle("DELETE /api/monitors/{id}", a.requireToken(a.handleDeleteMonitor))
	mux.Handle("POST /api/monitors/{id}/check", a.requireToken(a.handleCheckMonitor))
	mux.Handle("GET /api/monitors/{id}/incidents", a.requireToken(a.handleIncidents))
	// Catches any /api/* request that didn't match a route above, so it
	// never falls through to the SPA handler mounted at "/" and returns
	// index.html for a typo'd or unsupported API path.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Recurso no encontrado")
	})

	return mux
}

// requireToken rejects any request whose "Authorization: Bearer <token>"
// header doesn't match the configured shared secret, using a constant-time
// comparison so response timing can't be used to guess the token.
func (a *api) requireToken(next http.HandlerFunc) http.Handler {
	const prefix = "Bearer "
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if !strings.HasPrefix(got, prefix) ||
			subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(got, prefix)), []byte(a.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "Token de autenticación inválido o ausente")
			return
		}
		next(w, r)
	})
}

func (a *api) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "commit": a.commit, "build": a.build})
}

type monitorsResponse struct {
	IntervalSeconds int               `json:"intervalSeconds"`
	Days            int               `json:"days"`
	History         string            `json:"history"`
	Monitors        []monitorResponse `json:"monitors"`
}

type monitorResponse struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description"`
	URL           string       `json:"url"`
	Service       *string      `json:"service"`
	Kind          string       `json:"kind"`
	Environment   *string      `json:"environment"`
	Status        string       `json:"status"`
	Checking      bool         `json:"checking"`
	LastCheck     *lastCheck   `json:"lastCheck"`
	Commit        *string      `json:"commit"`
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

	history := "full"
	if raw := r.URL.Query().Get("history"); raw != "" {
		if raw != "full" && raw != "recent" {
			writeError(w, http.StatusBadRequest, "El parámetro history debe ser full o recent")
			return
		}
		history = raw
	}

	monitors, err := a.store.ListMonitors(r.Context())
	if err != nil {
		writeInternalError(w, err)
		return
	}

	today := time.Now().In(a.loc)
	since := today.AddDate(0, 0, -(days - 1))

	// One query for every monitor's history instead of one query per
	// monitor: this endpoint is polled every few seconds by every open
	// dashboard tab, so its query count can't grow with monitor count.
	allStats, err := a.store.ListAllDailyStatsSince(r.Context(), since)
	if err != nil {
		writeInternalError(w, err)
		return
	}
	statsByMonitor := make(map[uuid.UUID][]db.DailyStat, len(monitors))
	for _, s := range allStats {
		statsByMonitor[s.MonitorID] = append(statsByMonitor[s.MonitorID], s)
	}

	resp := monitorsResponse{
		IntervalSeconds: int(a.interval.Seconds()),
		Days:            days,
		History:         history,
		Monitors:        make([]monitorResponse, 0, len(monitors)),
	}
	for _, m := range monitors {
		mr := buildMonitorResponseFromStats(m, days, today, statsByMonitor[m.ID], a.scheduler.IsChecking(m.ID))
		if history == "recent" {
			mr = trimToRecentHistory(mr)
		}
		resp.Monitors = append(resp.Monitors, mr)
	}

	writeJSON(w, http.StatusOK, resp)
}

type createMonitorRequest struct {
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	Service     *string `json:"service"`
	Kind        *string `json:"kind"`
	Environment *string `json:"environment"`
}

// maxServiceLength caps a monitor's service name, in characters (not bytes,
// so accented names get the same room as plain ones).
const maxServiceLength = 80

// validateLabels checks already-normalized labels (service trimmed, absent
// or null values turned into "") and returns the Spanish message to send
// with a 400, or "" when they are all valid.
func validateLabels(l store.MonitorLabels) string {
	if utf8.RuneCountInString(l.Service) > maxServiceLength {
		return "El servicio no puede superar los 80 caracteres"
	}
	if l.Kind != "front" && l.Kind != "api" {
		return "El tipo debe ser front o api"
	}
	switch l.Environment {
	case "", "dev", "qa", "prod":
	default:
		return "El ambiente debe ser dev, qa o prod"
	}
	return ""
}

// labelsOf returns m's current labels, as validateLabels expects them.
func labelsOf(m db.Monitor) store.MonitorLabels {
	return store.MonitorLabels{
		Service:     m.Service.String,
		Kind:        m.Kind,
		Environment: m.Environment.String,
	}
}

func (a *api) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var req createMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "El cuerpo de la solicitud no es JSON válido")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, http.StatusBadRequest, "Ingresa un nombre para el monitor")
		return
	}
	if !isValidMonitorURL(req.URL) {
		writeError(w, http.StatusBadRequest, "Ingresa una URL válida (http o https)")
		return
	}

	// Absent or null labels fall back to no service, kind "api" and no
	// environment.
	labels := store.MonitorLabels{Kind: "api"}
	if req.Service != nil {
		labels.Service = strings.TrimSpace(*req.Service)
	}
	if req.Kind != nil {
		labels.Kind = *req.Kind
	}
	if req.Environment != nil {
		labels.Environment = *req.Environment
	}
	if msg := validateLabels(labels); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	created, err := a.store.CreateMonitor(r.Context(), name, req.Description, req.URL, labels)
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

type updateMonitorRequest struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Service     optionalString `json:"service"`
	Kind        optionalString `json:"kind"`
	Environment optionalString `json:"environment"`
}

// optionalString is a PATCH body field that tells an absent field (Set is
// false: keep the stored value) apart from an explicit null (Set is true and
// Value is nil: clear it). encoding/json calls UnmarshalJSON only for fields
// present in the body, including when their value is null.
type optionalString struct {
	Set   bool
	Value *string
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

// apply returns current when the field was absent, "" when it was null, and
// the sent value otherwise.
func (o optionalString) apply(current string) string {
	if !o.Set {
		return current
	}
	if o.Value == nil {
		return ""
	}
	return *o.Value
}

// handleUpdateMonitor renames a monitor, changes its description and sets
// any of its labels (service, kind, environment) present in the body; a
// label absent from the body keeps its stored value. Its URL is deliberately
// not editable here (see Store.UpdateMonitorDetails).
func (a *api) handleUpdateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMonitorID(w, r)
	if !ok {
		return
	}

	var req updateMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "El cuerpo de la solicitud no es JSON válido")
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		writeError(w, http.StatusBadRequest, "Ingresa un nombre para el monitor")
		return
	}

	current, err := a.store.GetMonitor(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "El monitor no existe")
			return
		}
		writeInternalError(w, err)
		return
	}
	labels := labelsOf(current)
	labels.Service = strings.TrimSpace(req.Service.apply(labels.Service))
	labels.Kind = req.Kind.apply(labels.Kind)
	labels.Environment = req.Environment.apply(labels.Environment)
	if msg := validateLabels(labels); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	m, err := a.store.UpdateMonitorDetails(r.Context(), id, name, req.Description, labels)
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
	today := time.Now().In(a.loc)
	since := today.AddDate(0, 0, -(days - 1))

	stats, err := a.store.ListDailyStatsSince(ctx, m.ID, since)
	if err != nil {
		return monitorResponse{}, err
	}
	return buildMonitorResponseFromStats(m, days, today, stats, a.scheduler.IsChecking(m.ID)), nil
}

// buildMonitorResponseFromStats does the actual assembly: it's pure (no I/O,
// no dependency on *api), so buildMonitorResponse (one monitor, its own
// query), handleListMonitors (every monitor, one shared batched query) and
// Publisher.MonitorChecked (after a check completes, for the SSE hub) can
// all use it. checking is passed in rather than read from a Scheduler here
// on purpose: Publisher has no Scheduler to ask (see events.go), and always
// means "false" for it anyway, since a MonitorChecked event by definition
// means a check just finished.
func buildMonitorResponseFromStats(m db.Monitor, days int, today time.Time, stats []db.DailyStat, checking bool) monitorResponse {
	resp := monitorResponse{
		ID:          m.ID.String(),
		Name:        m.Name,
		Description: m.Description,
		URL:         m.Url,
		Kind:        m.Kind,
		Status:      m.Status,
		Checking:    checking,
	}
	if m.Service.Valid {
		resp.Service = &m.Service.String
	}
	if m.Environment.Valid {
		resp.Environment = &m.Environment.String
	}

	if m.LastCheckedAt.Valid {
		resp.LastCheck = &lastCheck{
			At:         m.LastCheckedAt.Time.Format(time.RFC3339),
			HTTPStatus: int(m.LastHttpStatus.Int32),
			LatencyMs:  int(m.LastLatencyMs.Int32),
			ErrorKind:  m.LastErrorKind.String,
		}
	}
	if m.LastCommit.Valid {
		resp.Commit = &m.LastCommit.String
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

	return resp
}

// trimToRecentHistory keeps only the last recentHistoryDays entries of mr's
// History (already built oldest-first, with "today" anchored to the app's
// configured timezone), so a `history=recent` list response or an SSE event
// carries only the days that can still change. uptimePercent is left as-is:
// it was already computed over the full window the caller requested.
func trimToRecentHistory(mr monitorResponse) monitorResponse {
	if len(mr.History) > recentHistoryDays {
		mr.History = mr.History[len(mr.History)-recentHistoryDays:]
	}
	return mr
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
