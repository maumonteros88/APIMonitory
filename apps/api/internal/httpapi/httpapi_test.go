package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"apimonitory/internal/events"
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// fakeStore is an in-memory Store implementation so these tests never
// touch Postgres.
type fakeStore struct {
	monitors      map[uuid.UUID]db.Monitor
	allDailyStats []db.DailyStat
	dailyStats    map[uuid.UUID][]db.DailyStat // per-monitor override for ListDailyStatsSince
	checks        map[uuid.UUID][]fakeCheck    // per-monitor fixture for ListScheduledChecksBetween
}

// fakeCheck is a fakeStore fixture row carrying the fields
// ListScheduledChecksBetween filters on (monitor id via the map key,
// scheduled, checked_at) plus the ones its result row exposes (checked_at,
// status), so tests can seed unscheduled and out-of-range rows to prove the
// filter actually excludes them rather than returning everything.
type fakeCheck struct {
	CheckedAt time.Time
	Status    *string // nil means a NULL status (legacy pre-migration row)
	Scheduled bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{monitors: map[uuid.UUID]db.Monitor{}}
}

func (f *fakeStore) CreateMonitor(_ context.Context, name, description, url string) (db.Monitor, error) {
	m := db.Monitor{ID: uuid.New(), Name: name, Description: description, Url: url, Status: "pending"}
	f.monitors[m.ID] = m
	return m, nil
}

func (f *fakeStore) ListMonitors(_ context.Context) ([]db.Monitor, error) {
	out := make([]db.Monitor, 0, len(f.monitors))
	for _, m := range f.monitors {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeStore) GetMonitor(_ context.Context, id uuid.UUID) (db.Monitor, error) {
	m, ok := f.monitors[id]
	if !ok {
		return db.Monitor{}, store.ErrNotFound
	}
	return m, nil
}

func (f *fakeStore) UpdateMonitorDetails(_ context.Context, id uuid.UUID, name, description string) (db.Monitor, error) {
	m, ok := f.monitors[id]
	if !ok {
		return db.Monitor{}, store.ErrNotFound
	}
	m.Name = name
	m.Description = description
	f.monitors[id] = m
	return m, nil
}

func (f *fakeStore) DeleteMonitor(_ context.Context, id uuid.UUID) error {
	if _, ok := f.monitors[id]; !ok {
		return store.ErrNotFound
	}
	delete(f.monitors, id)
	return nil
}

func (f *fakeStore) ListDailyStatsSince(_ context.Context, id uuid.UUID, _ time.Time) ([]db.DailyStat, error) {
	return f.dailyStats[id], nil
}

func (f *fakeStore) ListAllDailyStatsSince(_ context.Context, _ time.Time) ([]db.DailyStat, error) {
	return f.allDailyStats, nil
}

func (f *fakeStore) ListScheduledChecksBetween(_ context.Context, id uuid.UUID, from, to time.Time) ([]db.ListScheduledChecksBetweenRow, error) {
	var out []db.ListScheduledChecksBetweenRow
	for _, c := range f.checks[id] {
		if !c.Scheduled || c.CheckedAt.Before(from) || !c.CheckedAt.Before(to) {
			continue
		}
		status := pgtype.Text{}
		if c.Status != nil {
			status = pgtype.Text{String: *c.Status, Valid: true}
		}
		out = append(out, db.ListScheduledChecksBetweenRow{
			CheckedAt: pgtype.Timestamptz{Time: c.CheckedAt, Valid: true},
			Status:    status,
		})
	}
	return out, nil
}

// fakeScheduler never performs real probes.
type fakeScheduler struct{}

func (fakeScheduler) RunManualCheck(_ context.Context, id uuid.UUID) (db.Monitor, error) {
	return db.Monitor{ID: id, Status: "pending"}, nil
}

func (fakeScheduler) IsChecking(uuid.UUID) bool { return false }

const testToken = "test-token"

// testMaxGap mirrors the maxGap main.go computes from real config, just
// large enough that ordinary test fixtures (checks a minute or so apart)
// never trip the gap-breaks-a-run behavior incidental to these tests.
const testMaxGap = 3 * time.Minute

func newTestMux(s *fakeStore) *http.ServeMux {
	return newTestMuxWithHub(s, events.NewHub())
}

func newTestMuxWithHub(s *fakeStore, hub *events.Hub) *http.ServeMux {
	return New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, hub, time.Millisecond*20, testMaxGap)
}

// authedRequest builds a request carrying the shared-secret bearer token, as
// every /api/* route other than /healthz now requires.
func authedRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	return req
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	msg, ok := body["error"]
	if !ok {
		t.Fatalf("response has no \"error\" field: %v", body)
	}
	return msg
}

func TestCreateMonitor_EmptyName(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(`{"name":"","url":"https://example.com"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if msg := decodeError(t, rec); msg == "" {
		t.Errorf("expected a non-empty error message")
	}
}

func TestCreateMonitor_InvalidURL(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(`{"name":"My API","url":"not-a-url"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if msg := decodeError(t, rec); msg == "" {
		t.Errorf("expected a non-empty error message")
	}
}

func TestCreateMonitor_InvalidURLScheme(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(`{"name":"My API","url":"ftp://example.com"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateMonitor_Success(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(`{"name":"My API","url":"https://example.com/health"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDeleteMonitor_NotFound(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodDelete, "/api/monitors/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteMonitor_InvalidID(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodDelete, "/api/monitors/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if msg := decodeError(t, rec); msg == "" {
		t.Errorf("expected a non-empty error message")
	}
}

func TestDeleteMonitor_Success(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap)

	req := authedRequest(http.MethodDelete, "/api/monitors/"+created.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestUpdateMonitor_Success(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "old desc", "https://example.com")
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap)

	req := authedRequest(
		http.MethodPatch,
		"/api/monitors/"+created.ID.String(),
		strings.NewReader(`{"name":"Renamed API","description":"new desc"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var got monitorResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Name != "Renamed API" || got.Description != "new desc" {
		t.Fatalf("got name=%q description=%q, want %q/%q", got.Name, got.Description, "Renamed API", "new desc")
	}
}

func TestUpdateMonitor_EmptyName(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap)

	req := authedRequest(
		http.MethodPatch,
		"/api/monitors/"+created.ID.String(),
		strings.NewReader(`{"name":"","description":"x"}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUpdateMonitor_NotFound(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(
		http.MethodPatch,
		"/api/monitors/"+uuid.New().String(),
		strings.NewReader(`{"name":"Renamed API","description":""}`),
	)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateMonitor_InvalidID(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPatch, "/api/monitors/not-a-uuid", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListMonitors_IncludesCommit(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	m := s.monitors[created.ID]
	m.LastCommit = pgtype.Text{String: "abc1234", Valid: true}
	s.monitors[created.ID] = m
	mux := newTestMux(s)

	req := authedRequest(http.MethodGet, "/api/monitors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got monitorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Monitors) != 1 || got.Monitors[0].Commit == nil || *got.Monitors[0].Commit != "abc1234" {
		t.Fatalf("got monitors=%+v, want one monitor with commit=abc1234", got.Monitors)
	}
}

// TestListMonitors_GroupsStatsPerMonitor guards the one risk introduced by
// batching the daily_stats query for the whole list into a single call: that
// grouping the flat result by monitor_id could cross-attribute one
// monitor's stats to another.
func TestListMonitors_GroupsStatsPerMonitor(t *testing.T) {
	s := newFakeStore()
	a1, _ := s.CreateMonitor(context.Background(), "API A", "", "https://a.example.com")
	a2, _ := s.CreateMonitor(context.Background(), "API B", "", "https://b.example.com")

	today := pgtype.Date{Time: time.Now().UTC(), Valid: true}
	s.allDailyStats = []db.DailyStat{
		// B listed first, so an order-dependent bug in the grouping would
		// still be exercised.
		{MonitorID: a2.ID, Day: today, Checks: 1, DownSeconds: 3600},
		{MonitorID: a1.ID, Day: today, Checks: 1, DownSeconds: 0},
	}

	mux := newTestMux(s)
	req := authedRequest(http.MethodGet, "/api/monitors?days=30", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got monitorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Monitors) != 2 {
		t.Fatalf("got %d monitors, want 2", len(got.Monitors))
	}

	byID := make(map[string]monitorResponse, 2)
	for _, m := range got.Monitors {
		byID[m.ID] = m
	}

	if up := byID[a1.ID.String()].UptimePercent; up == nil || *up != 100 {
		t.Errorf("monitor A uptime = %v, want 100 (no downtime of its own)", up)
	}
	if up := byID[a2.ID.String()].UptimePercent; up == nil || *up >= 100 {
		t.Errorf("monitor B uptime = %v, want <100 (has an hour of downtime)", up)
	}
}

func TestListMonitors_InvalidDays(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodGet, "/api/monitors?days=45", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHealthz(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownAPIPath_ReturnsJSON404NotSPA(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if msg := decodeError(t, rec); msg == "" {
		t.Errorf("expected a JSON error body, got: %s", rec.Body.String())
	}
}

func TestCheckMonitor_InvalidID(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodPost, "/api/monitors/not-a-uuid/check", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListMonitors_MissingToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/monitors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if msg := decodeError(t, rec); msg == "" {
		t.Errorf("expected a non-empty error message")
	}
}

func TestListMonitors_WrongToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/api/monitors", nil)
	req.Header.Set("Authorization", "Bearer not-the-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestDeleteMonitor_MissingToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodDelete, "/api/monitors/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, want auth to be checked before existence", rec.Code)
	}
}

func TestHealthz_DoesNotRequireToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (healthz must stay open for load balancer probes)", rec.Code)
	}
}

func TestListMonitors_NoHistoryParamDefaultsToFull(t *testing.T) {
	s := newFakeStore()
	if _, err := s.CreateMonitor(context.Background(), "My API", "", "https://example.com"); err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}
	mux := newTestMux(s)

	req := authedRequest(http.MethodGet, "/api/monitors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var got monitorsResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.History != "full" {
		t.Errorf("History = %q, want full", got.History)
	}
	if len(got.Monitors) != 1 || len(got.Monitors[0].History) != defaultHistoryDays {
		t.Fatalf("got %d monitors, want 1 with %d days of history", len(got.Monitors), defaultHistoryDays)
	}
}

func TestListMonitors_InvalidHistory(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := authedRequest(http.MethodGet, "/api/monitors?history=foo", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestListMonitors_HistoryRecent guards the trimmed payload against the full
// one: a history=recent response must be exactly the trailing
// recentHistoryDays entries of the equivalent full response, with the same
// uptimePercent (still computed over the full requested window), and it
// must declare itself via the History field rather than leaving the client
// to infer full vs. recent from the array length.
func TestListMonitors_HistoryRecent(t *testing.T) {
	s := newFakeStore()
	created, err := s.CreateMonitor(context.Background(), "My API", "", "https://example.com")
	if err != nil {
		t.Fatalf("CreateMonitor: %v", err)
	}

	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1)
	s.allDailyStats = []db.DailyStat{
		{MonitorID: created.ID, Day: pgtype.Date{Time: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, Checks: 1, DownSeconds: 60, Fails: 1},
		{MonitorID: created.ID, Day: pgtype.Date{Time: time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, Checks: 1},
	}
	mux := newTestMux(s)

	fullRec := httptest.NewRecorder()
	mux.ServeHTTP(fullRec, authedRequest(http.MethodGet, "/api/monitors", nil))
	var full monitorsResponse
	if err := json.NewDecoder(fullRec.Body).Decode(&full); err != nil {
		t.Fatalf("decode full response: %v", err)
	}
	if full.History != "full" || len(full.Monitors) != 1 || len(full.Monitors[0].History) != defaultHistoryDays {
		t.Fatalf("full response = %+v, want history=full with 1 monitor and %d days", full, defaultHistoryDays)
	}

	recentRec := httptest.NewRecorder()
	mux.ServeHTTP(recentRec, authedRequest(http.MethodGet, "/api/monitors?history=recent", nil))
	var recent monitorsResponse
	if err := json.NewDecoder(recentRec.Body).Decode(&recent); err != nil {
		t.Fatalf("decode recent response: %v", err)
	}
	if recent.History != "recent" {
		t.Errorf("History = %q, want recent", recent.History)
	}
	if len(recent.Monitors) != 1 {
		t.Fatalf("got %d monitors, want 1", len(recent.Monitors))
	}

	wantHistory := full.Monitors[0].History[len(full.Monitors[0].History)-recentHistoryDays:]
	if !reflect.DeepEqual(recent.Monitors[0].History, wantHistory) {
		t.Errorf("recent history = %+v, want the last %d entries of the full response: %+v",
			recent.Monitors[0].History, recentHistoryDays, wantHistory)
	}
	if recent.Monitors[0].UptimePercent == nil || full.Monitors[0].UptimePercent == nil ||
		*recent.Monitors[0].UptimePercent != *full.Monitors[0].UptimePercent {
		t.Errorf("uptimePercent mismatch: recent=%v, full=%v", recent.Monitors[0].UptimePercent, full.Monitors[0].UptimePercent)
	}
}

// TestBuildMonitorResponseFromStats_TrimToRecentHistory_CutsInART is the
// deterministic day-rollover check the plan calls for: "today" 22:30 in
// America/Argentina/Buenos_Aires is already past midnight UTC, so a cut that
// used the UTC calendar day (instead of the app's configured timezone) would
// leak tomorrow's (empty) day into the trimmed window instead of today's.
func TestBuildMonitorResponseFromStats_TrimToRecentHistory_CutsInART(t *testing.T) {
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	// 2026-09-26 22:30 ART == 2026-09-27 01:30 UTC.
	today := time.Date(2026, 9, 26, 22, 30, 0, 0, loc)

	mr := buildMonitorResponseFromStats(db.Monitor{ID: uuid.New(), Status: "up"}, defaultHistoryDays, today, nil, false)
	trimmed := trimToRecentHistory(mr)

	if len(trimmed.History) != recentHistoryDays {
		t.Fatalf("len(History) = %d, want %d", len(trimmed.History), recentHistoryDays)
	}
	wantDates := []string{"2026-09-25", "2026-09-26"}
	for i, want := range wantDates {
		if trimmed.History[i].Date != want {
			t.Errorf("History[%d].Date = %q, want %q (never 2026-09-27)", i, trimmed.History[i].Date, want)
		}
	}
}
