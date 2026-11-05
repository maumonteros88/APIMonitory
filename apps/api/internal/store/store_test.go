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

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health", MonitorLabels{Kind: "api"})
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

	created, err := s.CreateMonitor(ctx, "original name", "original desc", "https://example.com/health", MonitorLabels{Kind: "api"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	updated, err := s.UpdateMonitorDetails(ctx, created.ID, "renamed", "new desc", MonitorLabels{Service: "Pagos", Kind: "front", Environment: "prod"})
	if err != nil {
		t.Fatalf("UpdateMonitorDetails: %v", err)
	}
	if updated.Name != "renamed" || updated.Description != "new desc" {
		t.Errorf("got name=%q description=%q, want %q/%q", updated.Name, updated.Description, "renamed", "new desc")
	}
	if updated.Url != created.Url {
		t.Errorf("url changed to %q, want it untouched (%q)", updated.Url, created.Url)
	}
	if updated.Service.String != "Pagos" || !updated.Service.Valid || updated.Kind != "front" ||
		updated.Environment.String != "prod" || !updated.Environment.Valid {
		t.Errorf("labels = %+v/%q/%+v, want Pagos/front/prod", updated.Service, updated.Kind, updated.Environment)
	}

	// Empty service and environment are stored as NULL, not as "".
	cleared, err := s.UpdateMonitorDetails(ctx, created.ID, "renamed", "new desc", MonitorLabels{Kind: "api"})
	if err != nil {
		t.Fatalf("UpdateMonitorDetails (clear labels): %v", err)
	}
	if cleared.Service.Valid || cleared.Environment.Valid || cleared.Kind != "api" {
		t.Errorf("labels = %+v/%q/%+v, want NULL/api/NULL", cleared.Service, cleared.Kind, cleared.Environment)
	}

	if _, err := s.UpdateMonitorDetails(ctx, uuid.New(), "x", "y", MonitorLabels{Kind: "api"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateMonitorDetails on missing id: err = %v, want ErrNotFound", err)
	}
}

func TestStore_CreateMonitor_Labels(t *testing.T) {
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

	created, err := s.CreateMonitor(ctx, "test monitor", "", "https://example.com", MonitorLabels{Service: "Backoffice", Kind: "front", Environment: "qa"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	got, err := s.GetMonitor(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMonitor: %v", err)
	}
	if got.Service.String != "Backoffice" || got.Kind != "front" || got.Environment.String != "qa" {
		t.Errorf("labels = %+v/%q/%+v, want Backoffice/front/qa", got.Service, got.Kind, got.Environment)
	}

	// The CHECK constraints are the last line of defense behind the HTTP
	// validation.
	for _, labels := range []MonitorLabels{{Kind: "web"}, {Kind: "api", Environment: "staging"}} {
		if m, err := s.CreateMonitor(ctx, "test monitor", "", "https://example.com", labels); err == nil {
			s.DeleteMonitor(ctx, m.ID)
			t.Errorf("CreateMonitor(%+v) succeeded, want a check constraint violation", labels)
		}
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

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health", MonitorLabels{Kind: "api"})
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

	m1, err := s.CreateMonitor(ctx, "monitor A", "", "https://a.example.com/health", MonitorLabels{Kind: "api"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, m1.ID)

	m2, err := s.CreateMonitor(ctx, "monitor B", "", "https://b.example.com/health", MonitorLabels{Kind: "api"})
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

// TestStore_RegisterAttempt_PersistsStatusAndCheckedAt guards the checks
// columns the incidents endpoint depends on: status must round-trip, and
// checked_at must be exactly a.Now (truncated to microseconds, since
// timestamptz has no nanosecond precision) rather than whatever now() on the
// server happened to be a few ms later.
func TestStore_RegisterAttempt_PersistsStatusAndCheckedAt(t *testing.T) {
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

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health", MonitorLabels{Kind: "api"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Now().In(loc).Truncate(time.Microsecond).Add(123 * time.Nanosecond)

	err = s.RegisterAttempt(ctx, Attempt{
		MonitorID:     created.ID,
		Result:        monitor.ProbeResult{Success: false, ErrorKind: "timeout"},
		NewStatus:     monitor.StatusDown,
		NewFailures:   2,
		Scheduled:     true,
		CheckInterval: 60 * time.Second,
		Now:           now,
	})
	if err != nil {
		t.Fatalf("RegisterAttempt: %v", err)
	}

	rows, err := s.ListScheduledChecksBetween(ctx, created.ID, now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListScheduledChecksBetween: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !rows[0].Status.Valid || rows[0].Status.String != "down" {
		t.Errorf("status = %+v, want down", rows[0].Status)
	}
	if !rows[0].CheckedAt.Time.Equal(now.Truncate(time.Microsecond)) {
		t.Errorf("checked_at = %v, want %v", rows[0].CheckedAt.Time, now.Truncate(time.Microsecond))
	}
}

// TestStore_ListScheduledChecksBetween_FiltersAndOrders guards the three
// things buildIncidents relies on the query to already have done: manual
// checks excluded, [from, to) bounds (inclusive start, exclusive end), and
// ascending order by checked_at regardless of insertion order.
func TestStore_ListScheduledChecksBetween_FiltersAndOrders(t *testing.T) {
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

	created, err := s.CreateMonitor(ctx, "test monitor", "created by store_test", "https://example.com/health", MonitorLabels{Kind: "api"})
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	defer s.DeleteMonitor(ctx, created.ID)

	base := time.Now().UTC().Truncate(time.Second)
	attempt := func(offset time.Duration, scheduled bool) Attempt {
		return Attempt{
			MonitorID:     created.ID,
			Result:        monitor.ProbeResult{Success: true, HTTPStatus: 200, LatencyMs: 10},
			NewStatus:     monitor.StatusUp,
			Scheduled:     scheduled,
			CheckInterval: 60 * time.Second,
			Now:           base.Add(offset),
		}
	}

	// Inserted out of chronological order on purpose, to prove the query
	// orders by checked_at rather than insertion order.
	for _, a := range []Attempt{
		attempt(2*time.Second, true),  // in range
		attempt(0, true),              // exactly at from: included ([from,to))
		attempt(-time.Second, true),   // before from: excluded
		attempt(1*time.Second, false), // manual: excluded regardless of range
		attempt(4*time.Second, true),  // exactly at to: excluded ([from,to))
		attempt(3*time.Second, true),  // in range
	} {
		if err := s.RegisterAttempt(ctx, a); err != nil {
			t.Fatalf("RegisterAttempt(offset): %v", err)
		}
	}

	rows, err := s.ListScheduledChecksBetween(ctx, created.ID, base, base.Add(4*time.Second))
	if err != nil {
		t.Fatalf("ListScheduledChecksBetween: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3: %+v", len(rows), rows)
	}
	wantOffsets := []time.Duration{0, 2 * time.Second, 3 * time.Second}
	for i, want := range wantOffsets {
		if !rows[i].CheckedAt.Time.Equal(base.Add(want)) {
			t.Errorf("rows[%d].CheckedAt = %v, want %v (ascending order)", i, rows[i].CheckedAt.Time, base.Add(want))
		}
	}
}

func TestStore_ListCheckBuckets(t *testing.T) {
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

	mk := func(name string) uuid.UUID {
		m, err := s.CreateMonitor(ctx, name, "created by store_test", "https://example.com/health", MonitorLabels{Kind: "api"})
		if err != nil {
			t.Fatalf("CreateMonitor: %v", err)
		}
		t.Cleanup(func() { s.DeleteMonitor(ctx, m.ID) })
		return m.ID
	}
	pagos, cobros, other := mk("Pagos API"), mk("Cobros API"), mk("Reportes API")

	// 15-minute buckets from an epoch-aligned instant.
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	const bucketSeconds = 900
	add := func(id uuid.UUID, at time.Time, scheduled, success bool, latencyMs int, status monitor.Status) {
		t.Helper()
		res := monitor.ProbeResult{Success: success, LatencyMs: latencyMs}
		if !success {
			res.ErrorKind = "timeout"
		}
		err := s.RegisterAttempt(ctx, Attempt{
			MonitorID: id, Result: res, NewStatus: status, Scheduled: scheduled,
			CheckInterval: time.Minute, Now: at,
		})
		if err != nil {
			t.Fatalf("RegisterAttempt: %v", err)
		}
	}

	// Bucket 0: two ups plus one just before the boundary (sub-second, to
	// catch float rounding), and a manual failure that must not count.
	add(pagos, base.Add(1*time.Minute), true, true, 100, monitor.StatusUp)
	add(pagos, base.Add(2*time.Minute), true, true, 300, monitor.StatusUp)
	add(pagos, base.Add(3*time.Minute), false, false, 9000, monitor.StatusDown)
	add(pagos, base.Add(15*time.Minute-time.Millisecond), true, true, 50, monitor.StatusUp)
	// Bucket 1: starts with a check exactly on the boundary (belongs here, not
	// to bucket 0); a failure then a success, so worst is down but latency
	// only reflects the success.
	add(pagos, base.Add(15*time.Minute), true, false, 5000, monitor.StatusDown)
	add(pagos, base.Add(17*time.Minute), true, true, 200, monitor.StatusUp)
	// One microsecond before the next boundary still belongs to bucket 1.
	add(pagos, base.Add(30*time.Minute-time.Microsecond), true, true, 10, monitor.StatusUp)
	// Bucket 2: a degraded success.
	add(pagos, base.Add(30*time.Minute), true, true, 2500, monitor.StatusDegraded)
	// Bucket 3: a success exactly on the boundary, and a failed check.
	// Bucket 4 (base+1h) is the exclusive upper bound of the window below,
	// and base-1s the lower: both outside.
	add(pagos, base.Add(45*time.Minute), true, true, 30, monitor.StatusUp)
	add(pagos, base.Add(50*time.Minute), true, false, 5000, monitor.StatusDown)
	add(pagos, base.Add(time.Hour), true, true, 70, monitor.StatusUp)
	add(pagos, base.Add(-time.Second), true, true, 70, monitor.StatusUp)
	// Another monitor in the same bucket 0, and one that isn't requested.
	add(cobros, base.Add(5*time.Minute), true, true, 40, monitor.StatusUp)
	add(other, base.Add(5*time.Minute), true, true, 999, monitor.StatusUp)

	rows, err := s.ListCheckBuckets(ctx, []uuid.UUID{pagos, cobros}, base, base.Add(time.Hour), bucketSeconds)
	if err != nil {
		t.Fatalf("ListCheckBuckets: %v", err)
	}

	type key struct {
		id uuid.UUID
		at time.Time
	}
	got := map[key]db.ListCheckBucketsRow{}
	for _, r := range rows {
		got[key{r.MonitorID, r.Bucket.Time.UTC()}] = r
	}
	if len(rows) != 5 {
		t.Fatalf("got %d bucket rows, want 5 (4 for pagos, 1 for cobros): %+v", len(rows), rows)
	}

	// worst: down 3, degraded 2, up 1.
	want := []struct {
		id               uuid.UUID
		bucket           int
		checks, okChecks int32
		sumMs            int64
		maxMs, worst     int32
	}{
		{pagos, 0, 3, 3, 450, 300, 1},
		{pagos, 1, 3, 2, 210, 200, 3},
		{pagos, 2, 1, 1, 2500, 2500, 2},
		{pagos, 3, 2, 1, 30, 30, 3},
		{cobros, 0, 1, 1, 40, 40, 1},
	}
	for _, w := range want {
		at := base.Add(time.Duration(w.bucket*bucketSeconds) * time.Second)
		r, ok := got[key{w.id, at}]
		if !ok {
			t.Errorf("missing row for monitor %s bucket %d (%s)", w.id, w.bucket, at)
			continue
		}
		if r.Checks != w.checks || r.OkChecks != w.okChecks || r.OkLatencySumMs != w.sumMs || r.OkLatencyMaxMs != w.maxMs || r.Worst != w.worst {
			t.Errorf("monitor %s bucket %d = checks %d ok %d sum %d max %d worst %d, want %d/%d/%d/%d/%d",
				w.id, w.bucket, r.Checks, r.OkChecks, r.OkLatencySumMs, r.OkLatencyMaxMs, r.Worst,
				w.checks, w.okChecks, w.sumMs, w.maxMs, w.worst)
		}
	}
}
