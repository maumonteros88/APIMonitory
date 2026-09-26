package store

import (
	"context"
	"os"
	"testing"
	"time"

	"apimonitory/internal/monitor"
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
