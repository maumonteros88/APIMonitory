package events

import (
	"sync"
	"testing"
	"time"
)

func TestHub_PublishReachesAllSubscribers(t *testing.T) {
	h := NewHub()
	a := h.Subscribe()
	b := h.Subscribe()

	h.Publish(Event{Type: "monitor", Data: []byte("x")})

	for name, ch := range map[string]chan Event{"a": a, "b": b} {
		select {
		case ev := <-ch:
			if ev.Type != "monitor" || string(ev.Data) != "x" {
				t.Errorf("%s got %+v, want {monitor x}", name, ev)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: timed out waiting for event", name)
		}
	}
}

func TestHub_UnsubscribedReceivesNothing(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe()
	h.Unsubscribe(ch)

	h.Publish(Event{Type: "monitor", Data: []byte("x")})

	select {
	case ev, ok := <-ch:
		if ok {
			t.Errorf("got %+v after Unsubscribe, want nothing", ev)
		}
		// ok == false (channel closed elsewhere) would also be wrong here:
		// Unsubscribe must not close the channel either.
		t.Errorf("channel was closed after Unsubscribe, want it left open and simply forgotten")
	case <-time.After(50 * time.Millisecond):
		// no message, channel still open: correct.
	}
}

func TestHub_OverflowDisconnectsSlowSubscriber(t *testing.T) {
	h := NewHub()
	slow := h.Subscribe()
	fast := h.Subscribe()

	// Fill the slow subscriber's buffer without draining it, while draining
	// fast after every publish so it never fills up (a genuinely keeping-up
	// subscriber).
	for i := 0; i < subscriberBuffer+1; i++ {
		h.Publish(Event{Type: "monitor", Data: []byte("x")})
		<-fast
	}

	select {
	case _, ok := <-slow:
		if ok {
			// draining a buffered message is fine; keep draining until closed.
			for ok {
				_, ok = <-slow
			}
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber's channel was never closed after overflow")
	}

	if h.Len() != 1 {
		t.Errorf("Len() = %d after overflow, want 1 (only the fast subscriber left)", h.Len())
	}

	// The fast subscriber (drained every publish) must still be subscribed
	// and able to receive new events.
	h.Publish(Event{Type: "monitor", Data: []byte("y")})
	drained := 0
	for {
		select {
		case _, ok := <-fast:
			if !ok {
				t.Fatal("fast subscriber's channel was closed, want it untouched")
			}
			drained++
		default:
			if drained == 0 {
				t.Fatal("fast subscriber received nothing after overflow of a different subscriber")
			}
			return
		}
	}
}

func TestHub_Close(t *testing.T) {
	h := NewHub()
	a := h.Subscribe()
	b := h.Subscribe()

	h.Close()

	for name, ch := range map[string]chan Event{"a": a, "b": b} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("%s: got a value after Close, want closed channel", name)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: channel was not closed by Close", name)
		}
	}

	if h.Len() != 0 {
		t.Errorf("Len() = %d after Close, want 0", h.Len())
	}
}

func TestHub_SubscribeAfterCloseReturnsClosedChannel(t *testing.T) {
	h := NewHub()
	h.Close()

	ch := h.Subscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("got a value from Subscribe after Close, want an already-closed channel")
		}
	default:
		t.Error("channel from Subscribe after Close was not immediately closed")
	}
}

func TestHub_PublishAfterCloseDoesNotPanic(t *testing.T) {
	h := NewHub()
	h.Subscribe()
	h.Close()

	// Must not panic (send on closed channel) and must not re-add anyone.
	h.Publish(Event{Type: "monitor", Data: []byte("x")})
	if h.Len() != 0 {
		t.Errorf("Len() = %d after Publish post-Close, want 0", h.Len())
	}
}

// TestHub_ConcurrentUse runs many publishers against many
// subscribe/receive/unsubscribe goroutines at once; run with -race to catch
// data races in the shared subscriber map.
func TestHub_ConcurrentUse(t *testing.T) {
	h := NewHub()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.Publish(Event{Type: "monitor", Data: []byte("x")})
			}
		}()
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := h.Subscribe()
			defer h.Unsubscribe(ch)
			for j := 0; j < 10; j++ {
				select {
				case <-ch:
				case <-time.After(10 * time.Millisecond):
				}
			}
		}()
	}

	wg.Wait()
}
