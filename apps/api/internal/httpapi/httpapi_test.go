package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// fakeStore is an in-memory Store implementation so these tests never
// touch Postgres.
type fakeStore struct {
	monitors map[uuid.UUID]db.Monitor
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

func (f *fakeStore) ListDailyStatsSince(_ context.Context, _ uuid.UUID, _ time.Time) ([]db.DailyStat, error) {
	return nil, nil
}

// fakeScheduler never performs real probes.
type fakeScheduler struct{}

func (fakeScheduler) RunManualCheck(_ context.Context, id uuid.UUID) (db.Monitor, error) {
	return db.Monitor{ID: id, Status: "pending"}, nil
}

func (fakeScheduler) IsChecking(uuid.UUID) bool { return false }

const testToken = "test-token"

func newTestMux(s *fakeStore) *http.ServeMux {
	return New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken)
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
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken)

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
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken)

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
	mux := New(s, fakeScheduler{}, context.Background(), time.Minute, time.UTC, testToken)

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
