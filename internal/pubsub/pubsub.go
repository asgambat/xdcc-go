// Package pubsub provides a generic, thread-safe hub for fanning out events
// to multiple subscribers. It replaces the duplicated subscriberHub
// implementations previously scattered across ircmanager and queue.
package pubsub

import "sync"

// Hub manages a set of subscribers, fanning out published events to all of them.
// T is the event type.  bufSize controls the capacity of each subscriber's
// channel — when a subscriber's channel is full, new events are dropped
// (non-blocking fan-out).
type Hub[T any] struct {
	mu          sync.RWMutex
	subscribers []chan T
	bufSize     int
}

// New creates a Hub with the given per-subscriber buffer size.
func New[T any](bufSize int) *Hub[T] {
	return &Hub[T]{bufSize: bufSize}
}

// Subscribe adds a new subscriber and returns its event channel.
// The caller MUST drain the channel, otherwise events will be dropped
// once the buffer is full.
func (h *Hub[T]) Subscribe() chan T {
	ch := make(chan T, h.bufSize)
	h.mu.Lock()
	h.subscribers = append(h.subscribers, ch)
	h.mu.Unlock()
	return ch
}

// Unsubscribe removes a previously subscribed channel and closes it.
func (h *Hub[T]) Unsubscribe(ch chan T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, s := range h.subscribers {
		if s == ch {
			h.subscribers = append(h.subscribers[:i], h.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Publish fans out an event to all subscribers. If a subscriber's buffer is
// full, the event is silently dropped to prevent blocking the publisher.
func (h *Hub[T]) Publish(evt T) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.subscribers {
		select {
		case ch <- evt:
		default:
			// Drop event if subscriber is not consuming fast enough
		}
	}
}
