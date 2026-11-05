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
	bucketRows    []db.ListCheckBucketsRow     // fixture for ListCheckBuckets, filtered by ids and window
	bucketCalls   []bucketCall                 // every ListCheckBuckets call, for assertions
}

// bucketCall records the arguments of one ListCheckBuckets call.
type bucketCall struct {
	IDs           []uuid.UUID
	From, To      time.Time
	BucketSeconds int
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

func (f *fakeStore) CreateMonitor(_ context.Context, name, description, url string, labels store.MonitorLabels) (db.Monitor, error) {
	m := db.Monitor{ID: uuid.New(), Name: name, Description: description, Url: url, Status: "pending"}
	setLabels(&m, labels)
	f.monitors[m.ID] = m
	return m, nil
}

// setLabels mirrors how the real store persists labels: "" is stored as
// NULL for service, company and environment.
func setLabels(m *db.Monitor, labels store.MonitorLabels) {
	m.Service = pgtype.Text{String: labels.Service, Valid: labels.Service != ""}
	m.Company = pgtype.Text{String: labels.Company, Valid: labels.Company != ""}
	m.Kind = labels.Kind
	m.Environment = pgtype.Text{String: labels.Environment, Valid: labels.Environment != ""}
}

// defaultLabels is what POST /api/monitors stores when a body sends no
// labels, for fixtures that don't care about them.
var defaultLabels = store.MonitorLabels{Kind: "api"}

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

func (f *fakeStore) UpdateMonitorDetails(_ context.Context, id uuid.UUID, name, description string, labels store.MonitorLabels) (db.Monitor, error) {
	m, ok := f.monitors[id]
	if !ok {
		return db.Monitor{}, store.ErrNotFound
	}
	m.Name = name
	m.Description = description
	setLabels(&m, labels)
	f.monitors[id] = m
	return m, nil
}

func (f *fakeStore) AssignServiceCompany(_ context.Context, service, company string) (int64, error) {
	var n int64
	for id, m := range f.monitors {
		if m.Service.Valid && m.Service.String == service {
			m.Company = pgtype.Text{String: company, Valid: company != ""}
			f.monitors[id] = m
			n++
		}
	}
	return n, nil
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

func (f *fakeStore) ListCheckBuckets(_ context.Context, ids []uuid.UUID, from, to time.Time, bucketSeconds int) ([]db.ListCheckBucketsRow, error) {
	f.bucketCalls = append(f.bucketCalls, bucketCall{IDs: ids, From: from, To: to, BucketSeconds: bucketSeconds})
	want := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []db.ListCheckBucketsRow
	for _, r := range f.bucketRows {
		if want[r.MonitorID] && !r.Bucket.Time.Before(from) && r.Bucket.Time.Before(to) {
			out = append(out, r)
		}
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

const testCommit = "abc1234"

const testBuild = "deploy-20260101-000000-abc1234"

func newTestMux(s *fakeStore) *http.ServeMux {
	return newTestMuxWithHub(s, events.NewHub())
}

func newTestMuxWithHub(s *fakeStore, hub *events.Hub) *http.ServeMux {
	return New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, hub, time.Millisecond*20, testMaxGap, testCommit, testBuild)
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

// labelsJSON returns the raw JSON values of a monitor payload's service,
// kind and environment keys, so tests can tell null apart from "" or from a
// missing key.
func labelsJSON(t *testing.T, data []byte) [3]string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode monitor payload: %v", err)
	}
	var out [3]string
	for i, key := range []string{"service", "kind", "environment"} {
		v, ok := raw[key]
		if !ok {
			t.Fatalf("monitor payload has no %q key: %s", key, data)
		}
		out[i] = string(v)
	}
	return out
}

// companyJSON returns the raw JSON value of a monitor payload's company
// key, so tests can tell null apart from "" or from a missing key.
func companyJSON(t *testing.T, data []byte) string {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode monitor payload: %v", err)
	}
	v, ok := raw["company"]
	if !ok {
		t.Fatalf("monitor payload has no \"company\" key: %s", data)
	}
	return string(v)
}

const errCompany = "La empresa no puede superar los 80 caracteres"

func TestCreateMonitor_Company(t *testing.T) {
	accented80 := strings.Repeat("ñ", 80) // 160 bytes: the limit counts characters

	tests := []struct {
		name        string
		fields      string // fields appended to an otherwise valid body
		wantStatus  int
		wantCompany string // raw JSON
		wantError   string
	}{
		{"no company", ``, http.StatusCreated, `null`, ""},
		{"company", `,"company":"Empresa A"`, http.StatusCreated, `"Empresa A"`, ""},
		{"company is trimmed", `,"company":"  Empresa A  "`, http.StatusCreated, `"Empresa A"`, ""},
		{"empty company is null", `,"company":""`, http.StatusCreated, `null`, ""},
		{"blank company is null", `,"company":"   "`, http.StatusCreated, `null`, ""},
		{"null company is null", `,"company":null`, http.StatusCreated, `null`, ""},
		{"company at the limit", `,"company":"` + accented80 + `"`, http.StatusCreated, `"` + accented80 + `"`, ""},
		{"company over the limit", `,"company":"` + strings.Repeat("ñ", 81) + `"`, http.StatusBadRequest, "", errCompany},
		{"non-string company", `,"company":5`, http.StatusBadRequest, "", "El cuerpo de la solicitud no es JSON válido"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore()
			mux := newTestMux(s)

			body := `{"name":"Pagos API","url":"https://example.com/health"` + tc.fields + `}`
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantError != "" {
				if msg := decodeError(t, rec); msg != tc.wantError {
					t.Errorf("error = %q, want %q", msg, tc.wantError)
				}
				if len(s.monitors) != 0 {
					t.Errorf("store has %d monitors after a rejected create, want 0", len(s.monitors))
				}
				return
			}
			if got := companyJSON(t, rec.Body.Bytes()); got != tc.wantCompany {
				t.Errorf("company = %s, want %s", got, tc.wantCompany)
			}
		})
	}
}

// TestUpdateMonitor_Company covers PATCH's presence semantics for company:
// absent keeps it, a value replaces it (trimmed), null or "" clears it.
func TestUpdateMonitor_Company(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantCompany string // raw JSON, both in the response and in the store
		wantError   string
	}{
		{"absent company is unchanged", `{"name":"Pagos Web"}`, http.StatusOK, `"Empresa A"`, ""},
		{"set company", `{"name":"Pagos Web","company":"Empresa B"}`, http.StatusOK, `"Empresa B"`, ""},
		{"company is trimmed", `{"name":"Pagos Web","company":"  Empresa B "}`, http.StatusOK, `"Empresa B"`, ""},
		{"null company clears it", `{"name":"Pagos Web","company":null}`, http.StatusOK, `null`, ""},
		{"empty company clears it", `{"name":"Pagos Web","company":""}`, http.StatusOK, `null`, ""},
		{"company over the limit", `{"name":"Pagos Web","company":"` + strings.Repeat("a", 81) + `"}`, http.StatusBadRequest, `"Empresa A"`, errCompany},
		{"non-string company", `{"name":"Pagos Web","company":5}`, http.StatusBadRequest, `"Empresa A"`, "El cuerpo de la solicitud no es JSON válido"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore()
			created, _ := s.CreateMonitor(context.Background(), "Pagos Web", "", "https://example.com",
				store.MonitorLabels{Service: "Pagos", Company: "Empresa A", Kind: "front", Environment: "prod"})
			mux := newTestMux(s)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedRequest(http.MethodPatch, "/api/monitors/"+created.ID.String(), strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantError != "" {
				if msg := decodeError(t, rec); msg != tc.wantError {
					t.Errorf("error = %q, want %q", msg, tc.wantError)
				}
			} else if got := companyJSON(t, rec.Body.Bytes()); got != tc.wantCompany {
				t.Errorf("response company = %s, want %s", got, tc.wantCompany)
			}

			persisted, err := json.Marshal(buildMonitorResponseFromStats(s.monitors[created.ID], 1, time.Now(), nil, false))
			if err != nil {
				t.Fatalf("marshal stored monitor: %v", err)
			}
			if got := companyJSON(t, persisted); got != tc.wantCompany {
				t.Errorf("stored company = %s, want %s", got, tc.wantCompany)
			}
		})
	}
}

func TestAssignServiceCompany(t *testing.T) {
	accented80 := strings.Repeat("ñ", 80)

	tests := []struct {
		name        string
		body        string
		wantStatus  int
		wantResp    string // exact JSON of a 200 response
		wantCompany string // raw JSON of the "Pagos" monitors' company afterwards
		wantError   string
	}{
		{"sets the company", `{"service":"Pagos","company":"Empresa B"}`, http.StatusOK,
			`{"service":"Pagos","company":"Empresa B","updated":2}`, `"Empresa B"`, ""},
		{"service and company are trimmed", `{"service":"  Pagos ","company":"  Empresa B "}`, http.StatusOK,
			`{"service":"Pagos","company":"Empresa B","updated":2}`, `"Empresa B"`, ""},
		{"company at the limit", `{"service":"Pagos","company":"` + accented80 + `"}`, http.StatusOK,
			`{"service":"Pagos","company":"` + accented80 + `","updated":2}`, `"` + accented80 + `"`, ""},
		{"null company clears it", `{"service":"Pagos","company":null}`, http.StatusOK,
			`{"service":"Pagos","company":null,"updated":2}`, `null`, ""},
		{"empty company clears it", `{"service":"Pagos","company":""}`, http.StatusOK,
			`{"service":"Pagos","company":null,"updated":2}`, `null`, ""},
		{"absent company clears it", `{"service":"Pagos"}`, http.StatusOK,
			`{"service":"Pagos","company":null,"updated":2}`, `null`, ""},
		{"unknown service", `{"service":"Ejemplo","company":"Empresa B"}`, http.StatusNotFound,
			``, `"Empresa A"`, "No hay monitores en ese servicio"},
		{"empty service", `{"service":"","company":"Empresa B"}`, http.StatusBadRequest,
			``, `"Empresa A"`, "Ingresa el servicio"},
		{"blank service", `{"service":"   ","company":"Empresa B"}`, http.StatusBadRequest,
			``, `"Empresa A"`, "Ingresa el servicio"},
		{"absent service", `{"company":"Empresa B"}`, http.StatusBadRequest,
			``, `"Empresa A"`, "Ingresa el servicio"},
		{"company over the limit", `{"service":"Pagos","company":"` + strings.Repeat("ñ", 81) + `"}`, http.StatusBadRequest,
			``, `"Empresa A"`, errCompany},
		{"invalid JSON", `{"service":`, http.StatusBadRequest,
			``, `"Empresa A"`, "El cuerpo de la solicitud no es JSON válido"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore()
			labels := store.MonitorLabels{Service: "Pagos", Company: "Empresa A", Kind: "api"}
			a, _ := s.CreateMonitor(context.Background(), "Pagos API", "", "https://example.com", labels)
			b, _ := s.CreateMonitor(context.Background(), "Pagos Web", "", "https://example.com", labels)
			other, _ := s.CreateMonitor(context.Background(), "Backoffice API", "", "https://example.com",
				store.MonitorLabels{Service: "Backoffice", Company: "Empresa A", Kind: "api"})
			mux := newTestMux(s)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedRequest(http.MethodPatch, "/api/services/company", strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantError != "" {
				if msg := decodeError(t, rec); msg != tc.wantError {
					t.Errorf("error = %q, want %q", msg, tc.wantError)
				}
			} else if got := strings.TrimSpace(rec.Body.String()); got != tc.wantResp {
				t.Errorf("body = %s, want %s", got, tc.wantResp)
			}

			company := func(id uuid.UUID) string {
				persisted, err := json.Marshal(buildMonitorResponseFromStats(s.monitors[id], 1, time.Now(), nil, false))
				if err != nil {
					t.Fatalf("marshal stored monitor: %v", err)
				}
				return companyJSON(t, persisted)
			}
			for _, id := range []uuid.UUID{a.ID, b.ID} {
				if got := company(id); got != tc.wantCompany {
					t.Errorf("stored company = %s, want %s", got, tc.wantCompany)
				}
			}
			if got := company(other.ID); got != `"Empresa A"` {
				t.Errorf("another service's company = %s, want it untouched", got)
			}
		})
	}
}

func TestAssignServiceCompany_RequiresToken(t *testing.T) {
	mux := newTestMux(newFakeStore())

	req := httptest.NewRequest(http.MethodPatch, "/api/services/company", strings.NewReader(`{"service":"Pagos","company":"Empresa B"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestCreateMonitor_Labels(t *testing.T) {
	const (
		errService     = "El servicio no puede superar los 80 caracteres"
		errKind        = "El tipo debe ser front o api"
		errEnvironment = "El ambiente debe ser dev, qa, prepro o prod"
	)
	accented80 := strings.Repeat("ñ", 80) // 160 bytes: the limit counts characters

	tests := []struct {
		name       string
		fields     string // label fields appended to an otherwise valid body
		wantStatus int
		wantLabels [3]string // service, kind, environment as raw JSON
		wantError  string
	}{
		{"no labels", ``, http.StatusCreated, [3]string{`null`, `"api"`, `null`}, ""},
		{"all labels", `,"service":"Pagos","kind":"front","environment":"prod"`, http.StatusCreated, [3]string{`"Pagos"`, `"front"`, `"prod"`}, ""},
		{"service is trimmed", `,"service":"  Pagos  "`, http.StatusCreated, [3]string{`"Pagos"`, `"api"`, `null`}, ""},
		{"blank service is null", `,"service":"   "`, http.StatusCreated, [3]string{`null`, `"api"`, `null`}, ""},
		{"null labels use the defaults", `,"service":null,"kind":null,"environment":null`, http.StatusCreated, [3]string{`null`, `"api"`, `null`}, ""},
		{"empty environment is null", `,"environment":""`, http.StatusCreated, [3]string{`null`, `"api"`, `null`}, ""},
		{"service at the limit", `,"service":"` + accented80 + `"`, http.StatusCreated, [3]string{`"` + accented80 + `"`, `"api"`, `null`}, ""},
		{"service over the limit", `,"service":"` + strings.Repeat("a", 81) + `"`, http.StatusBadRequest, [3]string{}, errService},
		{"unknown kind", `,"kind":"web"`, http.StatusBadRequest, [3]string{}, errKind},
		{"empty kind", `,"kind":""`, http.StatusBadRequest, [3]string{}, errKind},
		{"unknown environment", `,"environment":"staging"`, http.StatusBadRequest, [3]string{}, errEnvironment},
		{"environment is case sensitive", `,"environment":"Prod"`, http.StatusBadRequest, [3]string{}, errEnvironment},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore()
			mux := newTestMux(s)

			body := `{"name":"Pagos API","url":"https://example.com/health"` + tc.fields + `}`
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedRequest(http.MethodPost, "/api/monitors", strings.NewReader(body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantError != "" {
				if msg := decodeError(t, rec); msg != tc.wantError {
					t.Errorf("error = %q, want %q", msg, tc.wantError)
				}
				if len(s.monitors) != 0 {
					t.Errorf("store has %d monitors after a rejected create, want 0", len(s.monitors))
				}
				return
			}
			if got := labelsJSON(t, rec.Body.Bytes()); got != tc.wantLabels {
				t.Errorf("labels = %v, want %v", got, tc.wantLabels)
			}
		})
	}
}

// TestUpdateMonitor_Labels covers PATCH's presence semantics: a label absent
// from the body keeps its stored value, a present one replaces it, and null
// or "" clears service and environment (kind can't be cleared).
func TestUpdateMonitor_Labels(t *testing.T) {
	stored := [3]string{`"Pagos"`, `"front"`, `"prod"`}

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantLabels [3]string // service, kind, environment as raw JSON
		wantError  string
	}{
		{"absent labels are unchanged", `{"name":"Pagos Web","description":"x"}`, http.StatusOK, stored, ""},
		{"set service", `{"name":"Pagos Web","service":"Backoffice"}`, http.StatusOK, [3]string{`"Backoffice"`, `"front"`, `"prod"`}, ""},
		{"service is trimmed", `{"name":"Pagos Web","service":"  Backoffice "}`, http.StatusOK, [3]string{`"Backoffice"`, `"front"`, `"prod"`}, ""},
		{"null service clears it", `{"name":"Pagos Web","service":null}`, http.StatusOK, [3]string{`null`, `"front"`, `"prod"`}, ""},
		{"empty service clears it", `{"name":"Pagos Web","service":""}`, http.StatusOK, [3]string{`null`, `"front"`, `"prod"`}, ""},
		{"set kind", `{"name":"Pagos Web","kind":"api"}`, http.StatusOK, [3]string{`"Pagos"`, `"api"`, `"prod"`}, ""},
		{"set environment", `{"name":"Pagos Web","environment":"qa"}`, http.StatusOK, [3]string{`"Pagos"`, `"front"`, `"qa"`}, ""},
		{"set prepro", `{"name":"Pagos Web","environment":"prepro"}`, http.StatusOK, [3]string{`"Pagos"`, `"front"`, `"prepro"`}, ""},
		{"null environment clears it", `{"name":"Pagos Web","environment":null}`, http.StatusOK, [3]string{`"Pagos"`, `"front"`, `null`}, ""},
		{"empty environment clears it", `{"name":"Pagos Web","environment":""}`, http.StatusOK, [3]string{`"Pagos"`, `"front"`, `null`}, ""},
		{"set every label", `{"name":"Pagos Web","service":"Ejemplo","kind":"api","environment":"dev"}`, http.StatusOK, [3]string{`"Ejemplo"`, `"api"`, `"dev"`}, ""},
		{"null kind", `{"name":"Pagos Web","kind":null}`, http.StatusBadRequest, stored, "El tipo debe ser front o api"},
		{"unknown kind", `{"name":"Pagos Web","kind":"web"}`, http.StatusBadRequest, stored, "El tipo debe ser front o api"},
		{"unknown environment", `{"name":"Pagos Web","environment":"staging"}`, http.StatusBadRequest, stored, "El ambiente debe ser dev, qa, prepro o prod"},
		{"service over the limit", `{"name":"Pagos Web","service":"` + strings.Repeat("a", 81) + `"}`, http.StatusBadRequest, stored, "El servicio no puede superar los 80 caracteres"},
		{"non-string service", `{"name":"Pagos Web","service":5}`, http.StatusBadRequest, stored, "El cuerpo de la solicitud no es JSON válido"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeStore()
			created, _ := s.CreateMonitor(context.Background(), "Pagos Web", "", "https://example.com",
				store.MonitorLabels{Service: "Pagos", Kind: "front", Environment: "prod"})
			mux := newTestMux(s)

			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, authedRequest(http.MethodPatch, "/api/monitors/"+created.ID.String(), strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantError != "" {
				if msg := decodeError(t, rec); msg != tc.wantError {
					t.Errorf("error = %q, want %q", msg, tc.wantError)
				}
			} else if got := labelsJSON(t, rec.Body.Bytes()); got != tc.wantLabels {
				t.Errorf("response labels = %v, want %v", got, tc.wantLabels)
			}

			// What the store kept, read back the same way the list does.
			persisted, err := json.Marshal(buildMonitorResponseFromStats(s.monitors[created.ID], 1, time.Now(), nil, false))
			if err != nil {
				t.Fatalf("marshal stored monitor: %v", err)
			}
			if got := labelsJSON(t, persisted); got != tc.wantLabels {
				t.Errorf("stored labels = %v, want %v", got, tc.wantLabels)
			}
		})
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
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap, testCommit, testBuild)

	req := authedRequest(http.MethodDelete, "/api/monitors/"+created.ID.String(), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestUpdateMonitor_Success(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "old desc", "https://example.com", defaultLabels)
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap, testCommit, testBuild)

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
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken, events.NewHub(), time.Millisecond*20, testMaxGap, testCommit, testBuild)

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
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)
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

func TestListMonitors_IncludesLabels(t *testing.T) {
	s := newFakeStore()
	labeled, _ := s.CreateMonitor(context.Background(), "Pagos API", "", "https://example.com",
		store.MonitorLabels{Service: "Pagos", Kind: "api", Environment: "qa"})
	bare, _ := s.CreateMonitor(context.Background(), "Ejemplo", "", "https://example.org", defaultLabels)
	mux := newTestMux(s)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, authedRequest(http.MethodGet, "/api/monitors", nil))

	var got struct {
		Monitors []json.RawMessage `json:"monitors"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := map[string][3]string{
		labeled.ID.String(): {`"Pagos"`, `"api"`, `"qa"`},
		bare.ID.String():    {`null`, `"api"`, `null`},
	}
	if len(got.Monitors) != len(want) {
		t.Fatalf("got %d monitors, want %d", len(got.Monitors), len(want))
	}
	for _, raw := range got.Monitors {
		var m struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("decode monitor: %v", err)
		}
		if labels := labelsJSON(t, raw); labels != want[m.ID] {
			t.Errorf("monitor %s labels = %v, want %v", m.ID, labels, want[m.ID])
		}
	}
}

// TestListMonitors_GroupsStatsPerMonitor guards the one risk introduced by
// batching the daily_stats query for the whole list into a single call: that
// grouping the flat result by monitor_id could cross-attribute one
// monitor's stats to another.
func TestListMonitors_GroupsStatsPerMonitor(t *testing.T) {
	s := newFakeStore()
	a1, _ := s.CreateMonitor(context.Background(), "API A", "", "https://a.example.com", defaultLabels)
	a2, _ := s.CreateMonitor(context.Background(), "API B", "", "https://b.example.com", defaultLabels)

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
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" || body["commit"] != testCommit ||
		body["build"] != testBuild {
		t.Fatalf("body = %v, want status ok, commit %q, build %q", body, testCommit, testBuild)
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
	if _, err := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels); err != nil {
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
	created, err := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)
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
