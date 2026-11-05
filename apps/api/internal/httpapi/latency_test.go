package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"apimonitory/internal/events"
	"apimonitory/internal/store/db"
)

// latencyNow is a fixed "now" in the middle of a 15-minute bucket, chosen so
// the 24h window matches the example in the endpoint's contract.
var latencyNow = time.Date(2026, 11, 6, 13, 40, 0, 0, time.UTC)

func newLatencyMux(s *fakeStore) *http.ServeMux {
	a := &api{
		store:        s,
		scheduler:    fakeScheduler{},
		bgCtx:        context.Background(),
		interval:     time.Minute,
		loc:          time.UTC,
		token:        testToken,
		hub:          events.NewHub(),
		sseHeartbeat: 20 * time.Millisecond,
		maxGap:       testMaxGap,
		now:          func() time.Time { return latencyNow },
	}
	return a.routes()
}

func getLatency(t *testing.T, mux *http.ServeMux, query string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/monitors/latency"+query, nil))
	return rec
}

func decodeLatency(t *testing.T, rec *httptest.ResponseRecorder) latencyResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var resp latencyResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func seedMonitors(s *fakeStore, names ...string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(names))
	for _, n := range names {
		m, _ := s.CreateMonitor(context.Background(), n, "", "https://example.com", defaultLabels)
		ids = append(ids, m.ID)
	}
	return ids
}

func bucketRow(id uuid.UUID, at time.Time, checks, okChecks int32, sumMs int64, maxMs, worst int32) db.ListCheckBucketsRow {
	return db.ListCheckBucketsRow{
		MonitorID:      id,
		Bucket:         pgtype.Timestamptz{Time: at, Valid: true},
		Checks:         checks,
		OkChecks:       okChecks,
		OkLatencySumMs: sumMs,
		OkLatencyMaxMs: maxMs,
		Worst:          worst,
	}
}

func TestLatency_RequiresToken(t *testing.T) {
	mux := newLatencyMux(newFakeStore())
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/monitors/latency", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// "latency" must reach the latency handler, not be parsed as a monitor id by
// a {id} route (which would answer 400 "El id no es un UUID válido").
func TestLatency_NotParsedAsMonitorID(t *testing.T) {
	rec := getLatency(t, newLatencyMux(newFakeStore()), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
}

func TestLatency_RangeValidation(t *testing.T) {
	s := newFakeStore()
	seedMonitors(s, "Pagos API")
	mux := newLatencyMux(s)

	cases := []struct {
		query      string
		wantStatus int
		wantRange  string
		wantBucket int
		wantPoints int
	}{
		{"", 200, "24h", 15, 96},
		{"?range=24h", 200, "24h", 15, 96},
		{"?range=7d", 200, "7d", 120, 84},
		{"?range=30d", 200, "30d", 720, 60},
		{"?range=1h", 400, "", 0, 0},
		{"?range=24H", 400, "", 0, 0},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			rec := getLatency(t, mux, c.query)
			if rec.Code != c.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, c.wantStatus)
			}
			if c.wantStatus == 400 {
				if msg := decodeError(t, rec); msg != "El parámetro range debe ser 24h, 7d o 30d" {
					t.Errorf("message = %q", msg)
				}
				return
			}
			resp := decodeLatency(t, rec)
			if resp.Range != c.wantRange || resp.BucketMinutes != c.wantBucket {
				t.Errorf("range=%q bucketMinutes=%d, want %q/%d", resp.Range, resp.BucketMinutes, c.wantRange, c.wantBucket)
			}
			if len(resp.Monitors) != 1 || len(resp.Monitors[0].Points) != c.wantPoints {
				t.Errorf("want 1 monitor with %d points, got %+v", c.wantPoints, resp.Monitors)
			}
		})
	}
}

func TestLatency_WindowAlignment(t *testing.T) {
	cases := []struct {
		query, from, to string
		bucketSeconds   int
	}{
		{"?range=24h", "2026-11-05T13:45:00Z", "2026-11-06T13:45:00Z", 900},
		{"?range=7d", "2026-10-30T14:00:00Z", "2026-11-06T14:00:00Z", 7200},
		{"?range=30d", "2026-10-08T00:00:00Z", "2026-11-07T00:00:00Z", 43200},
	}
	for _, c := range cases {
		t.Run(c.query, func(t *testing.T) {
			s := newFakeStore()
			seedMonitors(s, "Pagos API")
			resp := decodeLatency(t, getLatency(t, newLatencyMux(s), c.query))
			if resp.From != c.from || resp.To != c.to {
				t.Errorf("from/to = %s / %s, want %s / %s", resp.From, resp.To, c.from, c.to)
			}
			pts := resp.Monitors[0].Points
			if pts[0].At != c.from {
				t.Errorf("first point at %s, want %s", pts[0].At, c.from)
			}
			// The last bucket is the one containing now, and it ends at "to".
			last, _ := time.Parse(time.RFC3339, pts[len(pts)-1].At)
			to, _ := time.Parse(time.RFC3339, c.to)
			size := time.Duration(c.bucketSeconds) * time.Second
			if !last.Add(size).Equal(to) || latencyNow.Before(last) || !latencyNow.Before(to) {
				t.Errorf("last bucket %s does not contain now %s and end at %s", last, latencyNow, to)
			}
			if len(s.bucketCalls) != 1 || s.bucketCalls[0].BucketSeconds != c.bucketSeconds ||
				s.bucketCalls[0].From.Format(time.RFC3339) != c.from || s.bucketCalls[0].To.Format(time.RFC3339) != c.to {
				t.Errorf("store call = %+v", s.bucketCalls)
			}
		})
	}
}

func TestLatency_NowOnBucketBoundaryStartsNewBucket(t *testing.T) {
	from, to := latencyWindow(time.Date(2026, 11, 6, 13, 45, 0, 0, time.UTC), latencyRanges["24h"])
	if got := from.Format(time.RFC3339); got != "2026-11-05T14:00:00Z" {
		t.Errorf("from = %s", got)
	}
	if got := to.Format(time.RFC3339); got != "2026-11-06T14:00:00Z" {
		t.Errorf("to = %s", got)
	}
}

func TestLatency_IDsValidation(t *testing.T) {
	mux := newLatencyMux(newFakeStore())
	for _, q := range []string{
		"?ids=nope",
		"?ids=" + uuid.NewString() + ",nope",
		"?ids=" + uuid.NewString() + ",,",
	} {
		rec := getLatency(t, mux, q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
			continue
		}
		if msg := decodeError(t, rec); msg != "El parámetro ids tiene un id inválido" {
			t.Errorf("%s: message = %q", q, msg)
		}
	}
}

func TestLatency_FilterByIDsAndSkipUnknown(t *testing.T) {
	s := newFakeStore()
	ids := seedMonitors(s, "Pagos API", "Cobros API", "Reportes API")
	unknown := uuid.New()
	mux := newLatencyMux(s)

	resp := decodeLatency(t, getLatency(t, mux, "?ids="+ids[0].String()+","+unknown.String()+","+ids[0].String()))
	if len(resp.Monitors) != 1 || resp.Monitors[0].ID != ids[0].String() {
		t.Fatalf("monitors = %+v, want only %s", resp.Monitors, ids[0])
	}
	if got := s.bucketCalls[0].IDs; len(got) != 1 || got[0] != ids[0] {
		t.Errorf("store asked for ids %v, want only %s", got, ids[0])
	}

	// Only unknown ids: an empty list, and no pointless store query.
	s.bucketCalls = nil
	resp = decodeLatency(t, getLatency(t, mux, "?ids="+unknown.String()))
	if len(resp.Monitors) != 0 || len(s.bucketCalls) != 0 {
		t.Errorf("monitors = %+v, calls = %d, want none", resp.Monitors, len(s.bucketCalls))
	}

	// No ids: every monitor.
	resp = decodeLatency(t, getLatency(t, mux, ""))
	if len(resp.Monitors) != 3 {
		t.Errorf("got %d monitors, want 3", len(resp.Monitors))
	}
}

func TestLatency_FillsEmptyBucketsAndMapsRows(t *testing.T) {
	s := newFakeStore()
	ids := seedMonitors(s, "Pagos API", "Cobros API")
	// 24h window starts 2026-11-05T13:45Z; slot 0 and slot 95 (13:30 next day).
	first := time.Date(2026, 11, 5, 13, 45, 0, 0, time.UTC)
	last := time.Date(2026, 11, 6, 13, 30, 0, 0, time.UTC)
	s.bucketRows = []db.ListCheckBucketsRow{
		bucketRow(ids[0], first, 15, 15, 1800, 310, 1),                 // up
		bucketRow(ids[0], first.Add(15*time.Minute), 3, 0, 0, 0, 3),    // only failures: null latency, down
		bucketRow(ids[0], first.Add(30*time.Minute), 4, 3, 100, 60, 2), // degraded; avg 33.33 -> 33
		bucketRow(ids[0], first.Add(45*time.Minute), 2, 2, 5, 3, 1),    // avg 2.5 rounds to 3
		bucketRow(ids[0], last, 2, 2, 240, 130, 1),
		bucketRow(ids[1], first.Add(-15*time.Minute), 1, 1, 10, 10, 1), // outside the window: ignored
		bucketRow(ids[1], first.Add(60*time.Minute), 1, 0, 0, 0, 0),    // pending only: status none
	}

	resp := decodeLatency(t, getLatency(t, newLatencyMux(s), "?ids="+ids[0].String()+","+ids[1].String()))
	byID := map[string][]latencyPoint{}
	for _, m := range resp.Monitors {
		if len(m.Points) != 96 {
			t.Fatalf("monitor %s has %d points, want 96", m.ID, len(m.Points))
		}
		byID[m.ID] = m.Points
	}
	p := byID[ids[0].String()]

	for i, pt := range p {
		want := first.Add(time.Duration(i) * 15 * time.Minute).Format(time.RFC3339)
		if pt.At != want {
			t.Fatalf("point %d at %s, want %s (oldest first, contiguous)", i, pt.At, want)
		}
	}

	intp := func(v int) *int { return &v }
	check := func(i int, avg, max *int, checks int, status string) {
		t.Helper()
		g := p[i]
		eq := func(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
		if !eq(g.AvgMs, avg) || !eq(g.MaxMs, max) || g.Checks != checks || g.Status != status {
			t.Errorf("point %d = %+v (avg %v max %v), want avg %v max %v checks %d status %s", i, g, deref(g.AvgMs), deref(g.MaxMs), deref(avg), deref(max), checks, status)
		}
	}
	check(0, intp(120), intp(310), 15, "up")
	check(1, nil, nil, 3, "down")
	check(2, intp(33), intp(60), 4, "degraded")
	check(3, intp(3), intp(3), 2, "up")
	check(4, nil, nil, 0, "none") // an empty bucket
	check(95, intp(120), intp(130), 2, "up")

	q := byID[ids[1].String()]
	if q[0].Checks != 0 || q[0].Status != "none" {
		t.Errorf("out-of-window row leaked into slot 0: %+v", q[0])
	}
	if q[4].Checks != 1 || q[4].Status != "none" || q[4].AvgMs != nil || q[4].MaxMs != nil {
		t.Errorf("pending-only bucket = %+v, want 1 check, status none, null latencies", q[4])
	}
}

func TestLatency_EmptyBucketsSerializeAsNullAndNone(t *testing.T) {
	s := newFakeStore()
	seedMonitors(s, "Pagos API")
	rec := getLatency(t, newLatencyMux(s), "?range=30d")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `{"at":"2026-10-08T00:00:00Z","avgMs":null,"maxMs":null,"checks":0,"status":"none"}`) {
		t.Errorf("body does not contain the expected empty point: %.300s", body)
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
