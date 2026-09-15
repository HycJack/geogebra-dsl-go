package ai

import "sync"

// Session holds the ordered message history for one conversation so follow-up
// turns (e.g. "append a slider for point A") see what was generated before.
//
// A Session is safe for concurrent use by the HTTP layer: multiple /api/chat
// requests may target the same session, and history is mutated by each completed
// turn, so access is guarded by an internal mutex.
type Session struct {
	mu       sync.Mutex
	ID       string
	Messages []Message
	max      int
}

// NewSession creates an empty session with the given truncation cap.
func NewSession(id string, maxHistory int) *Session {
	if maxHistory < 1 {
		maxHistory = 1
	}
	return &Session{ID: id, max: maxHistory}
}

// Append adds a message to history, trimming from the front (past the system
// message, which is re-injected separately by the caller) to stay within cap.
func (s *Session) Append(m Message) {
	s.mu.Lock()
	s.Messages = append(s.Messages, m)
	s.trimLocked()
	s.mu.Unlock()
}

// trim keeps at most max user/assistant turns by dropping the oldest ones. The
// system prompt is not stored here (it is prepended per request), so trimming
// is a simple front-drop.
func (s *Session) trimLocked() {
	if s.max <= 0 || len(s.Messages) <= s.max {
		return
	}
	over := len(s.Messages) - s.max
	s.Messages = append([]Message(nil), s.Messages[over:]...)
}

// HasMessages reports whether any history exists.
func (s *Session) HasMessages() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Messages) > 0
}

// ContextMessages returns the full history for building a request (the system
// turn is assembled by the caller).
func (s *Session) ContextMessages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.Messages...)
}
