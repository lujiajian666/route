package events

import (
	"sync"
	"time"

	"be/internal/model"
)

// Log is a bounded in-memory ring buffer of audit events.
type Log struct {
	mu    sync.Mutex
	seq   int64
	max   int
	items []model.Event
}

// New creates an event log capped at max entries.
func New(max int) *Log {
	if max <= 0 {
		max = 500
	}
	return &Log{max: max}
}

// Add appends an event and returns it.
func (l *Log) Add(level, action, host, message, command string) model.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	ev := model.Event{
		ID:      l.seq,
		At:      time.Now(),
		Level:   level,
		Action:  action,
		Host:    host,
		Message: message,
		Command: command,
	}
	l.items = append(l.items, ev)
	if len(l.items) > l.max {
		l.items = append([]model.Event(nil), l.items[len(l.items)-l.max:]...)
	}
	return ev
}

// List returns the newest events first, capped at limit.
func (l *Log) List(limit int) []model.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.items) {
		limit = len(l.items)
	}
	out := make([]model.Event, 0, limit)
	for i := len(l.items) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, l.items[i])
	}
	return out
}
