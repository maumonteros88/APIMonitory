package httpapi

import (
	"errors"
	"net/http"
	"time"

	"apimonitory/internal/monitor"
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// incident is the pure (no I/O, no timezone formatting) representation of
// one detected down/degraded run within a queried day; the handler is the
// only place that converts Start/End to the app's configured timezone.
type incident struct {
	State   monitor.Status
	Start   time.Time
	End     time.Time
	Ongoing bool
}

type incidentResponse struct {
	State   string `json:"state"`
	Start   string `json:"start"`
	End     string `json:"end"`
	Ongoing bool   `json:"ongoing"`
}

type incidentsResponse struct {
	Day       string             `json:"day"`
	Incidents []incidentResponse `json:"incidents"`
	Partial   bool               `json:"partial"`
}

// handleIncidents returns the down/degraded time ranges for one monitor on
// one calendar day, so the day tooltip can show an exact "14:32 - 14:41"
// range instead of just aggregated minutes. day must be YYYY-MM-DD, no later
// than today and no older than defaultHistoryDays-1 days ago (both evaluated
// in a.loc, same convention as handleListMonitors' since).
func (a *api) handleIncidents(w http.ResponseWriter, r *http.Request) {
	id, ok := parseMonitorID(w, r)
	if !ok {
		return
	}

	raw := r.URL.Query().Get("day")
	dayStart, err := time.ParseInLocation("2006-01-02", raw, a.loc)
	if err != nil {
		writeError(w, http.StatusBadRequest, "El parámetro day debe tener el formato YYYY-MM-DD")
		return
	}

	now := time.Now().In(a.loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, a.loc)
	if dayStart.After(todayStart) {
		writeError(w, http.StatusBadRequest, "El parámetro day no puede ser una fecha futura")
		return
	}
	oldestAllowed := todayStart.AddDate(0, 0, -(defaultHistoryDays - 1))
	if dayStart.Before(oldestAllowed) {
		writeError(w, http.StatusBadRequest, "El parámetro day es demasiado antiguo")
		return
	}

	m, err := a.store.GetMonitor(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "El monitor no existe")
			return
		}
		writeInternalError(w, err)
		return
	}

	dayEnd := dayStart.AddDate(0, 0, 1)
	checks, err := a.store.ListScheduledChecksBetween(r.Context(), id, dayStart, dayEnd)
	if err != nil {
		writeInternalError(w, err)
		return
	}

	incidents, partial := buildIncidents(checks, a.interval, a.maxGap, dayEnd, now, monitor.Status(m.Status))

	resp := incidentsResponse{
		Day:       raw,
		Incidents: make([]incidentResponse, 0, len(incidents)),
		Partial:   partial,
	}
	for _, inc := range incidents {
		resp.Incidents = append(resp.Incidents, incidentResponse{
			State:   string(inc.State),
			Start:   inc.Start.In(a.loc).Format(time.RFC3339),
			End:     inc.End.In(a.loc).Format(time.RFC3339),
			Ongoing: inc.Ongoing,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

// buildIncidents groups an ascending, already-scheduled-only slice of checks
// into down/degraded runs. It performs no I/O so it can be exhaustively
// tested with a table of cases.
//
// A run is a maximal sequence of checks with the same status in
// {down, degraded}. Any check that doesn't extend the current run --
// up/pending, a NULL status (legacy row from before this migration), a
// different status, or a gap since the run's last check bigger than maxGap
// (the scheduler simply not having run for that long isn't the same as an
// uninterrupted outage) -- ends it, and NULL/up/pending never start a new
// one. A NULL-status row also marks partial, independent of runs.
//
// ongoing can only be true for the run left open at the end of the loop
// (the one containing the very last queried check, if any run is still
// open there at all), and only when its state still matches the monitor's
// live currentStatus and now is within maxGap of that last check --
// otherwise an outage that already resolved hours ago would look "in
// progress" the next time the tooltip is opened.
func buildIncidents(checks []db.ListScheduledChecksBetweenRow, interval, maxGap time.Duration, dayEnd, now time.Time, currentStatus monitor.Status) (incidents []incident, partial bool) {
	incidents = make([]incident, 0)

	var run *incident
	var runLast time.Time

	finalize := func(hasNext bool, nextAt time.Time) {
		if run == nil {
			return
		}
		end := runLast.Add(interval)
		if hasNext && nextAt.Before(end) {
			end = nextAt
		}
		if dayEnd.Before(end) {
			end = dayEnd
		}
		run.End = end
		incidents = append(incidents, *run)
		run = nil
	}

	for _, c := range checks {
		if !c.Status.Valid {
			partial = true
			finalize(true, c.CheckedAt.Time)
			continue
		}

		state := monitor.Status(c.Status.String)
		if run != nil && run.State == state && c.CheckedAt.Time.Sub(runLast) <= maxGap {
			runLast = c.CheckedAt.Time
			continue
		}

		finalize(true, c.CheckedAt.Time)

		if state == monitor.StatusDown || state == monitor.StatusDegraded {
			run = &incident{State: state, Start: c.CheckedAt.Time}
			runLast = c.CheckedAt.Time
		}
	}

	if run != nil {
		run.Ongoing = run.State == currentStatus && now.Sub(runLast) <= maxGap
	}
	finalize(false, time.Time{})

	return incidents, partial
}
