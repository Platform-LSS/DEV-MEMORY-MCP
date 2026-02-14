package web

import "sync"

// EventBus is an in-memory pub/sub for SSE events.
type EventBus struct {
	mu      sync.RWMutex
	clients map[chan string]struct{}
}

// NewEventBus creates a new event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		clients: make(map[chan string]struct{}),
	}
}

// Subscribe returns a channel that receives events and an unsubscribe function.
func (eb *EventBus) Subscribe() (chan string, func()) {
	ch := make(chan string, 16)
	eb.mu.Lock()
	eb.clients[ch] = struct{}{}
	eb.mu.Unlock()

	unsub := func() {
		eb.mu.Lock()
		delete(eb.clients, ch)
		eb.mu.Unlock()
		// Drain remaining messages
		for range ch {
		}
	}
	return ch, unsub
}

// Publish sends an event name to all subscribed clients.
func (eb *EventBus) Publish(event string) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for ch := range eb.clients {
		select {
		case ch <- event:
		default:
			// Client too slow, skip
		}
	}
}
