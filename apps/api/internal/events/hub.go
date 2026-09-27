// Package events provides a minimal in-memory publish/subscribe hub used to
// push updates to SSE clients. It knows nothing about monitors or JSON
// shapes on purpose: Event.Data is an opaque, already-serialized payload, so
// this package has no dependency on the domain and nothing importing it
// creates a cycle.
//
// This hub is in-memory and single-process only: a subscriber only sees
// events published from within the same process. If this app is ever run
// as more than one replica, publishing would need to fan out through a
// shared broker (e.g. Redis pub/sub) instead of this type.
package events

import "sync"

// Event is one message broadcast to every subscriber.
type Event struct {
	Type string
	Data []byte
}

// subscriberBuffer bounds how many undelivered events a slow subscriber can
// accumulate before Publish gives up on it (see Publish).
const subscriberBuffer = 8

// Hub fans out events to any number of subscribers.
type Hub struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

// NewHub builds an empty, open Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[chan Event]struct{})}
}

// Subscribe registers a new subscriber and returns its event channel. If the
// hub has already been closed (server shutting down), it returns an
// already-closed channel so the caller's receive loop exits immediately.
func (h *Hub) Subscribe() chan Event {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan Event, subscriberBuffer)
	if h.closed {
		close(ch)
		return ch
	}
	h.subs[ch] = struct{}{}
	return ch
}

// Unsubscribe removes ch from the hub. It only forgets the channel; it never
// closes it. The only goroutine still reading from ch is the one that owns
// it and is now leaving, so there is nothing left to signal, and closing it
// here could race with a concurrent Publish trying to send on it.
func (h *Hub) Unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, ch)
}

// Publish sends ev to every current subscriber without blocking. A
// subscriber that isn't keeping up (its buffer is full) is dropped and its
// channel closed, rather than silently missing events forever: the
// frontend's periodic reconciliation poll covers a dropped subscriber by
// reconnecting and re-fetching full state.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Close closes every current subscriber's channel and marks the hub closed:
// any later Subscribe returns an already-closed channel, and Publish becomes
// a no-op. Intended for http.Server.RegisterOnShutdown, so open SSE
// connections unblock immediately instead of stalling graceful shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.subs {
		close(ch)
		delete(h.subs, ch)
	}
}

// Len reports the current subscriber count.
func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
