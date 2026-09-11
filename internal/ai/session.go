package ai

// Session holds the ordered message history for one conversation so follow-up
// turns (e.g. "append a slider for point A") see what was generated before.
type Session struct {
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
	s.Messages = append(s.Messages, m)
	s.trim()
}

// trim keeps at most max user/assistant turns by dropping the oldest ones. The
// system prompt is not stored here (it is prepended per request), so trimming
// is a simple front-drop.
func (s *Session) trim() {
	if s.max <= 0 || len(s.Messages) <= s.max {
		return
	}
	over := len(s.Messages) - s.max
	s.Messages = append([]Message(nil), s.Messages[over:]...)
}

// HasMessages reports whether any history exists.
func (s *Session) HasMessages() bool { return len(s.Messages) > 0 }

// ContextMessages returns the full history for building a request (the system
// turn is assembled by the caller).
func (s *Session) ContextMessages() []Message {
	return append([]Message(nil), s.Messages...)
}
