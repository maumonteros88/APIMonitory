package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"apimonitory/internal/events"
	"apimonitory/internal/monitor"
	"apimonitory/internal/store/db"
)

// chk builds a scheduled-check fixture row with a valid status.
func chk(at time.Time, status monitor.Status) db.ListScheduledChecksBetweenRow {
	return db.ListScheduledChecksBetweenRow{
		CheckedAt: pgtype.Timestamptz{Time: at, Valid: true},
		Status:    pgtype.Text{String: string(status), Valid: true},
	}
}

// chkNull builds a legacy pre-migration fixture row with a NULL status.
func chkNull(at time.Time) db.ListScheduledChecksBetweenRow {
	return db.ListScheduledChecksBetweenRow{CheckedAt: pgtype.Timestamptz{Time: at, Valid: true}}
}

func incidentsEqual(t *testing.T, got, want []incident) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d incidents, want %d: got=%+v want=%+v", len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.State != w.State || !g.Start.Equal(w.Start) || !g.End.Equal(w.End) || g.Ongoing != w.Ongoing {
			t.Errorf("incident[%d] = %+v, want %+v", i, g, w)
		}
	}
}

func TestBuildIncidents_SimpleRun(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDown),
		chk(base.Add(time.Minute), monitor.StatusDown),
		chk(base.Add(2*time.Minute), monitor.StatusDown),
		chk(base.Add(3*time.Minute), monitor.StatusUp),
	}

	got, partial := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: base, End: base.Add(3 * time.Minute)},
	})
	if partial {
		t.Error("partial = true, want false")
	}
}

// TestBuildIncidents_SubThresholdFailureDoesNotBreakRun guards the case the
// plan calls out explicitly: a failed probe below FAILURE_THRESHOLD never
// gets its own persisted status (monitor.Next keeps the prior status), so a
// run of checks that are all "degraded" must stay one continuous run --
// there's no such thing as a stray "down" row silently appearing inside it
// that buildIncidents would need to special-case.
func TestBuildIncidents_SubThresholdFailureDoesNotBreakRun(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDegraded),
		chk(base.Add(time.Minute), monitor.StatusDegraded),
		chk(base.Add(2*time.Minute), monitor.StatusDegraded),
		chk(base.Add(3*time.Minute), monitor.StatusDegraded),
		chk(base.Add(4*time.Minute), monitor.StatusUp),
	}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDegraded, Start: base, End: base.Add(4 * time.Minute)},
	})
}

func TestBuildIncidents_AdjacentDifferentStatesDoNotMerge(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDegraded),
		chk(base.Add(time.Minute), monitor.StatusDegraded),
		chk(base.Add(2*time.Minute), monitor.StatusDown),
		chk(base.Add(3*time.Minute), monitor.StatusDown),
		chk(base.Add(4*time.Minute), monitor.StatusUp),
	}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDegraded, Start: base, End: base.Add(2 * time.Minute)},
		{State: monitor.StatusDown, Start: base.Add(2 * time.Minute), End: base.Add(4 * time.Minute)},
	})
}

func TestBuildIncidents_NullStatusBreaksRunAndMarksPartial(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDown),
		chk(base.Add(time.Minute), monitor.StatusDown),
		chkNull(base.Add(2 * time.Minute)),
		chk(base.Add(3*time.Minute), monitor.StatusDown),
		chk(base.Add(4*time.Minute), monitor.StatusDown),
		chk(base.Add(5*time.Minute), monitor.StatusUp),
	}

	got, partial := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: base, End: base.Add(2 * time.Minute)},
		{State: monitor.StatusDown, Start: base.Add(3 * time.Minute), End: base.Add(5 * time.Minute)},
	})
	if !partial {
		t.Error("partial = false, want true")
	}
}

func TestBuildIncidents_GapExactlyMaxGapDoesNotBreak(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDown),
		chk(base.Add(maxGap), monitor.StatusDown),
		chk(base.Add(maxGap+time.Minute), monitor.StatusUp),
	}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: base, End: base.Add(maxGap + time.Minute)},
	})
}

func TestBuildIncidents_GapOverMaxGapBreaks(t *testing.T) {
	base := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute
	overGap := maxGap + time.Second

	checks := []db.ListScheduledChecksBetweenRow{
		chk(base, monitor.StatusDown),
		chk(base.Add(overGap), monitor.StatusDown),
		chk(base.Add(overGap+time.Minute), monitor.StatusUp),
	}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, base.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: base, End: base.Add(interval)},
		{State: monitor.StatusDown, Start: base.Add(overGap), End: base.Add(overGap + time.Minute)},
	})
}

func TestBuildIncidents_EndClipsAtDayEnd(t *testing.T) {
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 1, 1, 23, 59, 30, 0, time.UTC) // + interval would cross into the next day
	interval := time.Minute
	maxGap := 3 * time.Minute

	checks := []db.ListScheduledChecksBetweenRow{chk(lastCheck, monitor.StatusDown)}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, lastCheck.Add(time.Hour), monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: lastCheck, End: dayEnd},
	})
}

func TestBuildIncidents_OngoingTrueWhenFreshAndStatusMatches(t *testing.T) {
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute
	now := lastCheck.Add(2 * time.Minute) // within maxGap

	checks := []db.ListScheduledChecksBetweenRow{chk(lastCheck, monitor.StatusDown)}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, now, monitor.StatusDown)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: lastCheck, End: lastCheck.Add(interval), Ongoing: true},
	})
}

func TestBuildIncidents_OngoingFalseWhenStale(t *testing.T) {
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute
	now := lastCheck.Add(time.Hour) // long past maxGap: this outage already resolved

	checks := []db.ListScheduledChecksBetweenRow{chk(lastCheck, monitor.StatusDown)}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, now, monitor.StatusDown)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: lastCheck, End: lastCheck.Add(interval), Ongoing: false},
	})
}

func TestBuildIncidents_OngoingFalseWhenCurrentStatusChanged(t *testing.T) {
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	lastCheck := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	interval := time.Minute
	maxGap := 3 * time.Minute
	now := lastCheck.Add(time.Minute) // within maxGap, but the monitor has since recovered

	checks := []db.ListScheduledChecksBetweenRow{chk(lastCheck, monitor.StatusDown)}

	got, _ := buildIncidents(checks, interval, maxGap, dayEnd, now, monitor.StatusUp)

	incidentsEqual(t, got, []incident{
		{State: monitor.StatusDown, Start: lastCheck, End: lastCheck.Add(interval), Ongoing: false},
	})
}

func TestBuildIncidents_EmptyList(t *testing.T) {
	dayEnd := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	got, partial := buildIncidents(nil, time.Minute, 3*time.Minute, dayEnd, dayEnd, monitor.StatusUp)

	if len(got) != 0 {
		t.Errorf("got %d incidents, want 0", len(got))
	}
	if partial {
		t.Error("partial = true, want false")
	}
}

// --- handler tests ---

func TestHandleIncidents_MissingDay(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := newTestMux(s)

	req := authedRequest(http.MethodGet, "/api/monitors/"+created.ID.String()+"/incidents", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleIncidents_MalformedDay(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := newTestMux(s)

	req := authedRequest(http.MethodGet, "/api/monitors/"+created.ID.String()+"/incidents?day=not-a-date", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleIncidents_FutureDay(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := newTestMux(s)

	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")
	req := authedRequest(http.MethodGet, "/api/monitors/"+created.ID.String()+"/incidents?day="+tomorrow, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleIncidents_TooOldDay(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := newTestMux(s)

	tooOld := time.Now().UTC().AddDate(0, 0, -defaultHistoryDays).Format("2006-01-02")
	req := authedRequest(http.MethodGet, "/api/monitors/"+created.ID.String()+"/incidents?day="+tooOld, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleIncidents_UnknownMonitor(t *testing.T) {
	mux := newTestMux(newFakeStore())

	today := time.Now().UTC().Format("2006-01-02")
	req := authedRequest(http.MethodGet, "/api/monitors/"+uuid.New().String()+"/incidents?day="+today, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleIncidents_MissingToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	today := time.Now().UTC().Format("2006-01-02")
	req := httptest.NewRequest(http.MethodGet, "/api/monitors/"+uuid.New().String()+"/incidents?day="+today, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestHandleIncidents_TimezoneCorrectness is the deterministic timezone check
// the plan calls for: a check at 23:00 in America/Argentina/Buenos_Aires
// (02:00Z the next UTC calendar day) must be attributed to ART's calendar
// day, not UTC's, and its start must render with the -03:00 offset.
func TestHandleIncidents_TimezoneCorrectness(t *testing.T) {
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, loc, testToken, events.NewHub(), time.Millisecond*20, testMaxGap)

	yesterday := time.Now().In(loc).AddDate(0, 0, -1)
	dayStr := yesterday.Format("2006-01-02")
	checkAt := time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 0, 0, 0, loc)

	s.checks = map[uuid.UUID][]fakeCheck{
		created.ID: {{CheckedAt: checkAt, Status: strPtr(string(monitor.StatusDown)), Scheduled: true}},
	}

	req := authedRequest(http.MethodGet, "/api/monitors/"+created.ID.String()+"/incidents?day="+dayStr, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	var got incidentsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Day != dayStr {
		t.Errorf("day = %q, want %q", got.Day, dayStr)
	}
	if len(got.Incidents) != 1 {
		t.Fatalf("got %d incidents, want 1: %+v", len(got.Incidents), got.Incidents)
	}
	wantStart := dayStr + "T23:00:00-03:00"
	if got.Incidents[0].Start != wantStart {
		t.Errorf("start = %q, want %q (ART calendar day and offset, not UTC)", got.Incidents[0].Start, wantStart)
	}
}

func strPtr(s string) *string { return &s }
