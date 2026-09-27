package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/monitor"
	"apimonitory/internal/store/db"
)

// TestStore_RegisterAttempt requires a live Postgres instance reachable via
// DATABASE_URL. It is skipped cleanly when that variable is unset so the
// rest of the suite (and CI without Postgres) is unaffected.
func TestStore_RegisterAttempt(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}

	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	if created.Status != "pending" {
		t.Errorf("initial status = %q, want pending", created.Status)
	}

	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Now().In(loc)

	err = s.RegisterAttempt(ctx, Attempt{
		MonitorID:     created.ID,
		Result:        monitor.ProbeResult{Success: false, ErrorKind: "timeout", LatencyMs: 5000},
		NewStatus:     monitor.StatusDown,
		NewFailures:   2,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("RegisterAttempt: %v", err)
	}

	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.Status != "down" {
		t.Errorf("status after attempt = %q, want down", got.Status)
	}
	if got.ConsecutiveFailures != 2 {
		t.Errorf("consecutive_failures = %d, want 2", got.ConsecutiveFailures)
	}
	if !got.LastErrorKind.Valid || got.LastErrorKind.String != "timeout" {
		t.Errorf("last_error_kind = %+v, want timeout", got.LastErrorKind)
	}

	stats, err := s.ListDailyStatsSince(ctx, created.ID, now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("ListDailyStatsSince: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("daily_stats rows = %d, want 1", len(stats))
	}
	if stats[0].Checks != 1 {
		t.Errorf("daily_stats.checks = %d, want 1", stats[0].Checks)
	}
	if stats[0].Fails != 1 {
		t.Errorf("daily_stats.fails = %d, want 1", stats[0].Fails)
	}
	if stats[0].DownSeconds != 60 {
		t.Errorf("daily_stats.down_seconds = %d, want 60", stats[0].DownSeconds)
	}
	if stats[0].DegradedSeconds != 0 {
		t.Errorf("daily_stats.degraded_seconds = %d, want 0", stats[0].DegradedSeconds)
	}
}

func TestStore_UpdateMonitorDetails(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}

	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	created, err := s.CreateMonitor(ctx, "original name", "original desc", "https://example.com/health")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	updated, err := s.UpdateMonitorDetails(ctx, created.ID, "renamed", "new desc")
	if err != nil {
		t.Fatalf("UpdateMonitorDetails: %v", err)
	}
	if updated.Name != "renamed" || updated.Description != "new desc" {
		t.Errorf("got name=%q description=%q, want %q/%q", updated.Name, updated.Description, "renamed", "new desc")
	}
	if updated.Url != created.Url {
		t.Errorf("url changed to %q, want it untouched (%q)", updated.Url, created.Url)
	}

	if _, err := s.UpdateMonitorDetails(ctx, uuid.New(), "x", "y"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateMonitorDetails on missing id: err = %v, want ErrNotFound", err)
	}
}

func TestStore_RegisterAttempt_PersistsCommit(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}

	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	err = s.RegisterAttempt(ctx, Attempt{
		MonitorID:     created.ID,
		Result:        monitor.ProbeResult{Success: true, HTTPStatus: 200, LatencyMs: 50, Commit: "abc1234"},
		NewStatus:     monitor.StatusUp,
		NewFailures:   0,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           time.Now(),
	})
	if err != nil {
		t.Fatalf("RegisterAttempt: %v", err)
	}

	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if !got.LastCommit.Valid || got.LastCommit.String != "abc1234" {
		t.Errorf("last_commit = %+v, want abc1234", got.LastCommit)
	}

	// A subsequent failed attempt has no commit and clears the previous one,
	// the same way last_http_status/last_error_kind already reflect only
	// the latest attempt rather than the last successful one.
	err = s.RegisterAttempt(ctx, Attempt{
		MonitorID:     created.ID,
		Result:        monitor.ProbeResult{Success: false, ErrorKind: "timeout"},
		NewStatus:     monitor.StatusUp,
		NewFailures:   1,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           time.Now(),
	})
	if err != nil {
		t.Fatalf("RegisterAttempt: %v", err)
	}

	got, err = s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.LastCommit.Valid {
		t.Errorf("last_commit = %+v, want cleared after a commit-less attempt", got.LastCommit)
	}
}

// TestStore_ListAllDailyStatsSince guards the batched query the list
// endpoint uses instead of one ListDailyStatsSince call per monitor: it
// must return every monitor's own rows, correctly attributed by monitor_id.
func TestStore_ListAllDailyStatsSince(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping store integration test")
	}

	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	m1, err := s.CreateMonitor(ctx, "monitor A", "", "https://a.example.com/health")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, m1.ID)

	m2, err := s.CreateMonitor(ctx, "monitor B", "", "https://b.example.com/health")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, m2.ID)

	now := time.Now()
	if err := s.RegisterAttempt(ctx, Attempt{
		MonitorID:     m1.ID,
		Result:        monitor.ProbeResult{Success: true, HTTPStatus: 200, LatencyMs: 10},
		NewStatus:     monitor.StatusUp,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           now,
	}); err != nil {
		t.Fatalf("RegisterAttempt(m1): %v", err)
	}
	if err := s.RegisterAttempt(ctx, Attempt{
		MonitorID:     m2.ID,
		Result:        monitor.ProbeResult{Success: false, ErrorKind: "timeout"},
		NewStatus:     monitor.StatusDown,
		NewFailures:   2,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           now,
	}); err != nil {
		t.Fatalf("RegisterAttempt(m2): %v", err)
	}

	stats, err := s.ListAllDailyStatsSince(ctx, now.AddDate(0, 0, -1))
	if err != nil {
		t.Fatalf("ListAllDailyStatsSince: %v", err)
	}

	byMonitor := make(map[uuid.UUID]db.DailyStat, len(stats))
	for _, row := range stats {
		byMonitor[row.MonitorID] = row
	}

	s1, ok := byMonitor[m1.ID]
	if !ok {
		t.Fatalf("no daily_stats row for m1")
	}
	if s1.DownSeconds != 0 {
		t.Errorf("m1 down_seconds = %d, want 0", s1.DownSeconds)
	}

	s2, ok := byMonitor[m2.ID]
	if !ok {
		t.Fatalf("no daily_stats row for m2")
	}
	if s2.DownSeconds != 60 {
		t.Errorf("m2 down_seconds = %d, want 60", s2.DownSeconds)
	}
}
