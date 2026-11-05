package httpapi

import (
	"bufio"
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
	"apimonitory/internal/store"
	"apimonitory/internal/store/db"
)

// sseFrame reads one SSE frame (up to the blank line that terminates it)
// from r and reports its event type and data, ignoring "retry:" lines and
// treating a comment line ("keep-alive") as event type "heartbeat".
// httptest.NewRecorder can't observe a handler's progressive writes as they
// happen (the handler runs to completion before the test ever reads the
// body), so streaming behavior here is tested against a real server and a
// real http.Client instead.
func sseFrame(t *testing.T, r *bufio.Reader) (eventType, data string) {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE frame: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return eventType, data
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		case strings.HasPrefix(line, ":"):
			eventType = "heartbeat"
		}
	}
}

func TestHandleEvents_RequiresToken(t *testing.T) {
	srv := httptest.NewServer(newTestMux(newFakeStore()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestHandleEvents_StreamsPublishedEvent(t *testing.T) {
	hub := events.NewHub()
	srv := httptest.NewServer(newTestMuxWithHub(newFakeStore(), hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	r := bufio.NewReader(resp.Body)

	// First frame is the initial "retry: 5000", proof the first flush
	// already happened by the time the client sees anything at all (and,
	// per handleEvents, that the subscription already exists too).
	if _, data := sseFrame(t, r); data != "" {
		t.Errorf("first frame data = %q, want empty (only a retry: line)", data)
	}

	hub.Publish(events.Event{Type: "monitor", Data: []byte(`{"id":"abc"}`)})

	eventType, data := sseFrame(t, r)
	if eventType != "monitor" || data != `{"id":"abc"}` {
		t.Errorf("got event=%q data=%q, want monitor/{\"id\":\"abc\"}", eventType, data)
	}
}

func TestHandleEvents_Heartbeat(t *testing.T) {
	hub := events.NewHub()
	// newTestMuxWithHub configures a 20ms heartbeat for exactly this.
	srv := httptest.NewServer(newTestMuxWithHub(newFakeStore(), hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	sseFrame(t, r) // discard the initial retry: line

	done := make(chan struct{})
	var eventType string
	go func() {
		eventType, _ = sseFrame(t, r)
		close(done)
	}()

	select {
	case <-done:
		if eventType != "heartbeat" {
			t.Errorf("got event=%q, want heartbeat", eventType)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a heartbeat")
	}
}

func TestHandleEvents_HubCloseEndsStream(t *testing.T) {
	hub := events.NewHub()
	srv := httptest.NewServer(newTestMuxWithHub(newFakeStore(), hub))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()

	r := bufio.NewReader(resp.Body)
	sseFrame(t, r) // discard the initial retry: line, proof we're subscribed

	hub.Close()

	done := make(chan error, 1)
	go func() {
		_, err := r.ReadString('\n')
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("read after hub.Close() succeeded, want the stream to end")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end within 2s of hub.Close() -- this is the regression test for the shutdown bug")
	}
}

func TestHandleEvents_ClientDisconnectUnsubscribes(t *testing.T) {
	hub := events.NewHub()
	srv := httptest.NewServer(newTestMuxWithHub(newFakeStore(), hub))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}

	r := bufio.NewReader(resp.Body)
	sseFrame(t, r) // discard the initial retry: line, proof we're subscribed
	if hub.Len() != 1 {
		t.Fatalf("hub.Len() = %d after connecting, want 1", hub.Len())
	}

	cancel()
	resp.Body.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Len() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub.Len() = %d 2s after client disconnect, want 0", hub.Len())
}

func TestPublisher_MonitorChecked_PublishesWithCheckingFalse(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)

	hub := events.NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	p := NewPublisher(s, time.UTC, hub)
	p.MonitorChecked(context.Background(), created.ID)

	select {
	case ev := <-ch:
		if ev.Type != "monitor" {
			t.Fatalf("event type = %q, want monitor", ev.Type)
		}
		var mr monitorResponse
		if err := json.Unmarshal(ev.Data, &mr); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if mr.ID != created.ID.String() {
			t.Errorf("id = %q, want %q", mr.ID, created.ID.String())
		}
		if mr.Checking {
			// This is the fix for the checking-flag race: a
			// MonitorChecked event means a check just finished, so it
			// must never say checking:true regardless of whether the
			// scheduler's in-flight guard has been released yet.
			t.Error("checking = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the published event")
	}
}

// TestPublisher_MonitorChecked_TrimsHistoryToRecentDays guards the SSE
// payload against the same 90-day leak the list endpoint's history=recent
// mode avoids: a published event must carry only the last recentHistoryDays
// days, even though it's built from (and its uptimePercent still reflects)
// the full defaultHistoryDays window.
func TestPublisher_MonitorChecked_TrimsHistoryToRecentDays(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "My API", "", "https://example.com", defaultLabels)

	today := time.Now().UTC()
	yesterday := today.AddDate(0, 0, -1)
	s.dailyStats = map[uuid.UUID][]db.DailyStat{
		created.ID: {
			{MonitorID: created.ID, Day: pgtype.Date{Time: time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, Checks: 1},
			{MonitorID: created.ID, Day: pgtype.Date{Time: time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, Checks: 1},
		},
	}

	hub := events.NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	p := NewPublisher(s, time.UTC, hub)
	p.MonitorChecked(context.Background(), created.ID)

	select {
	case ev := <-ch:
		var mr monitorResponse
		if err := json.Unmarshal(ev.Data, &mr); err != nil {
			t.Fatalf("unmarshal event data: %v", err)
		}
		if len(mr.History) != recentHistoryDays {
			t.Fatalf("len(History) = %d, want %d", len(mr.History), recentHistoryDays)
		}
		wantYesterday := yesterday.Format("2006-01-02")
		wantToday := today.Format("2006-01-02")
		if mr.History[0].Date != wantYesterday || mr.History[1].Date != wantToday {
			t.Errorf("history dates = %q, %q, want %q, %q", mr.History[0].Date, mr.History[1].Date, wantYesterday, wantToday)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the published event")
	}
}

func TestPublisher_MonitorChecked_IncludesLabels(t *testing.T) {
	s := newFakeStore()
	created, _ := s.CreateMonitor(context.Background(), "Backoffice Web", "", "https://example.com",
		store.MonitorLabels{Service: "Backoffice", Kind: "front", Environment: "dev"})

	hub := events.NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	p := NewPublisher(s, time.UTC, hub)
	p.MonitorChecked(context.Background(), created.ID)

	select {
	case ev := <-ch:
		want := [3]string{`"Backoffice"`, `"front"`, `"dev"`}
		if got := labelsJSON(t, ev.Data); got != want {
			t.Errorf("event labels = %v, want %v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the published event")
	}
}

func TestPublisher_MonitorChecked_NotFoundDoesNotPublish(t *testing.T) {
	hub := events.NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	p := NewPublisher(newFakeStore(), time.UTC, hub)
	p.MonitorChecked(context.Background(), uuid.New()) // not in the store

	select {
	case ev := <-ch:
		t.Fatalf("got event %+v for a monitor that doesn't exist, want none", ev)
	case <-time.After(100 * time.Millisecond):
		// no event: correct.
	}
}
