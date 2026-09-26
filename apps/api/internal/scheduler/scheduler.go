// Package scheduler runs periodic health probes against all monitors and
// exposes a synchronous manual-check path for the HTTP API.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/checker"
	"apimonitory/internal/config"
	"apimonitory/internal/monitor"
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// Scheduler periodically probes every monitor and also supports on-demand
// manual checks triggered by the HTTP API.
//
// Concurrency design: one shared semaphore (buffered channel) bounds how
// many probes run at once across every monitor, regardless of whether they
// were triggered by the periodic tick or a manual request. A per-monitor
// in-flight guard (sync.Map, using LoadOrStore to avoid a check-then-set
// race) ensures a monitor is never probed twice at once: if a scheduled
// cycle is already running for a monitor, that tick's probe for it is
// skipped (not queued); if a manual check is requested while one is
// already in flight for that monitor, the manual check is skipped too and
// simply returns the monitor's current state.
type Scheduler struct {
	store   *store.Store
	checker *checker.Checker

	interval         time.Duration
	retryDelay       time.Duration
	failureThreshold int
	degradedLatency  time.Duration
	loc              *time.Location

	sem      chan struct{}
	inFlight sync.Map // uuid.UUID -> struct{}
}

// New builds a Scheduler wired to the given store and checker, configured
// from cfg.
func New(s *store.Store, c *checker.Checker, cfg config.Config) *Scheduler {
	return &Scheduler{
		store:            s,
		checker:          c,
		interval:         cfg.CheckInterval,
		retryDelay:       cfg.RetryDelay,
		failureThreshold: cfg.FailureThreshold,
		degradedLatency:  cfg.DegradedLatency,
		loc:              cfg.AppTimezone,
		sem:              make(chan struct{}, cfg.MaxConcurrency),
	}
}

// Run ticks every CheckInterval, launching one probe per monitor per tick
// (subject to the in-flight guard and semaphore). It blocks until ctx is
// canceled, then waits for any probes already in flight to finish or abort
// before returning.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			s.tick(ctx, &wg)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, wg *sync.WaitGroup) {
	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		slog.Warn("scheduler: failed to list monitors", "error", err)
		return
	}

	for _, m := range monitors {
		id := m.ID
		if _, alreadyRunning := s.inFlight.LoadOrStore(id, struct{}{}); alreadyRunning {
			continue
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			defer s.inFlight.Delete(id)

			// Re-read after claiming the in-flight guard, not before: the
			// list above can go stale by the time this goroutine runs, and
			// probing with it directly would let a concurrent manual
			// check's fresher status/failure count get silently
			// overwritten (RunManualCheck already reads fresh the same
			// way, right after acquiring the same guard).
			fresh, err := s.store.GetMonitor(ctx, id)
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					slog.Warn("scheduler: failed to refresh monitor before scheduled check", "monitor_id", id, "error", err)
				}
				return
			}
			s.probeAndPersist(ctx, fresh, true)
		}()
	}
}

// RunManualCheck performs one on-demand, synchronous check for a single
// monitor. If a check (scheduled or manual) is already in flight for this
// monitor, it does not run a new probe and simply returns the monitor's
// current state; callers can use IsChecking to tell the two cases apart.
func (s *Scheduler) RunManualCheck(ctx context.Context, id uuid.UUID) (db.Monitor, error) {
	if _, alreadyRunning := s.inFlight.LoadOrStore(id, struct{}{}); alreadyRunning {
		return s.store.GetMonitor(ctx, id)
	}
	defer s.inFlight.Delete(id)

	m, err := s.store.GetMonitor(ctx, id)
	if err != nil {
		return db.Monitor{}, err
	}

	s.probeAndPersist(ctx, m, false)

	return s.store.GetMonitor(ctx, id)
}

// IsChecking reports whether a probe is currently in flight for id.
func (s *Scheduler) IsChecking(id uuid.UUID) bool {
	_, ok := s.inFlight.Load(id)
	return ok
}

// probeAndPersist runs the probe-retry-classify cycle for one monitor and
// persists the result. On failure it retries once after RetryDelay before
// deciding the final outcome (the anti-false-positive policy lives in
// monitor.Next, which additionally requires FailureThreshold consecutive
// failed attempts before flipping a monitor to down). The concurrency
// semaphore is released during the retry sleep and re-acquired only for
// the retry probe itself, so a slow batch of confirmation retries can't
// starve other monitors' checks (or a synchronous manual check) out of
// every slot for the whole RetryDelay.
func (s *Scheduler) probeAndPersist(ctx context.Context, m db.Monitor, scheduled bool) {
	if !s.acquireSem(ctx) {
		return
	}
	result := s.checker.Probe(ctx, m.Url)
	s.releaseSem()

	currentStatus := monitor.Status(m.Status)
	if !result.Success && currentStatus != monitor.StatusDown {
		select {
		case <-time.After(s.retryDelay):
		case <-ctx.Done():
			return
		}

		if !s.acquireSem(ctx) {
			return
		}
		result = s.checker.Probe(ctx, m.Url)
		s.releaseSem()
	}

	newStatus, newFailures := monitor.Next(
		currentStatus,
		int(m.ConsecutiveFailures),
		result,
		s.degradedLatency,
		s.failureThreshold,
	)

	err := s.store.RegisterAttempt(ctx, store.Attempt{
		MonitorID:     m.ID,
		Result:        result,
		NewStatus:     newStatus,
		NewFailures:   newFailures,
		Scheduled:     scheduled,
		CheckInterval: s.interval,
		Now:           time.Now().In(s.loc),
	})
	if err != nil {
		slog.Warn("scheduler: failed to persist check attempt", "monitor_id", m.ID, "error", err)
	}
}

// acquireSem blocks for a concurrency slot, returning false if ctx is
// canceled first.
func (s *Scheduler) acquireSem(ctx context.Context) bool {
	select {
	case s.sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseSem frees a slot acquired via acquireSem.
func (s *Scheduler) releaseSem() { <-s.sem }
