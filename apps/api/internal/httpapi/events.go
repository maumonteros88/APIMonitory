package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/events"
	"apimonitory/internal/store"
)

// sseSubscriberCap bounds how many /api/events connections can be open at
// once, so a single shared token can't be used to open unbounded goroutines.
const sseSubscriberCap = 100

// handleEvents streams one "monitor" event per check that completes,
// carrying that monitor's full public JSON shape (the same shape a REST
// fetch would return), plus a periodic keep-alive comment. It never uses
// EventSource on the frontend side (see api.ts): the client authenticates
// with the same Authorization header as every other route, so this goes
// through the same requireToken middleware unchanged.
func (a *api) handleEvents(w http.ResponseWriter, r *http.Request) {
	if a.hub.Len() >= sseSubscriberCap {
		writeError(w, http.StatusServiceUnavailable, "Demasiadas conexiones de eventos activas")
		return
	}

	rc := http.NewResponseController(w)

	// Subscribed before anything is written, so that the moment the client
	// sees any output at all (including this first flush), the hub is
	// already guaranteed to deliver events to it -- no window where a
	// Publish between "headers sent" and "subscribed" would be missed.
	ch := a.hub.Subscribe()
	defer a.hub.Unsubscribe(ch)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // hint for a future reverse proxy: don't buffer this response
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, "retry: 5000\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	ticker := time.NewTicker(a.sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				// The hub was closed (server shutting down) or this
				// subscriber was dropped for not keeping up.
				return
			}
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, ev.Data); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-ticker.C:
			_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// Publisher builds a monitor's public JSON shape after a check completes and
// broadcasts it over the hub. It's a separate, lightweight type rather than
// a method on *api on purpose: scheduler.New needs a scheduler.Notifier
// before httpapi.New can build the *api that would otherwise implement it
// (httpapi.New itself takes the already-built Scheduler) -- wiring a
// Publisher that only needs a Store, a location and the Hub breaks that
// construction cycle. See cmd/api/main.go for the wiring order.
type Publisher struct {
	store Store
	loc   *time.Location
	hub   *events.Hub
}

// NewPublisher builds a Publisher. s and loc are the same Store and
// timezone httpapi.New is given; hub is shared with it too.
func NewPublisher(s Store, loc *time.Location, hub *events.Hub) *Publisher {
	return &Publisher{store: s, loc: loc, hub: hub}
}

// MonitorChecked implements scheduler.Notifier. It never fails the check it
// was called from: any error here is logged and swallowed.
func (p *Publisher) MonitorChecked(ctx context.Context, id uuid.UUID) {
	m, err := p.store.GetMonitor(ctx, id)
	if err != nil {
		// Deleted between the check completing and this read: expected,
		// not worth a warning (the scheduler's own tick treats it the
		// same way).
		if !errors.Is(err, store.ErrNotFound) {
			slog.Warn("httpapi: publisher failed to load monitor after check", "monitor_id", id, "error", err)
		}
		return
	}

	today := time.Now().In(p.loc)
	since := today.AddDate(0, 0, -(defaultHistoryDays - 1))
	stats, err := p.store.ListDailyStatsSince(ctx, id, since)
	if err != nil {
		slog.Warn("httpapi: publisher failed to load daily stats after check", "monitor_id", id, "error", err)
		return
	}

	mr := buildMonitorResponseFromStats(m, defaultHistoryDays, today, stats, false)
	data, err := json.Marshal(mr)
	if err != nil {
		slog.Warn("httpapi: publisher failed to marshal monitor event", "monitor_id", id, "error", err)
		return
	}

	p.hub.Publish(events.Event{Type: "monitor", Data: data})
}
