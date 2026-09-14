package ai

import "fmt"

// LogFunc receives one structured log event emitted by the service layer. It is
// invoked synchronously and must be safe to call from multiple goroutines (the
// HTTP layer can process concurrent requests). A nil LogFunc disables logging.
type LogFunc func(Event string, fields map[string]any)

// logEvent calls the configured hook (if any) with a fresh fields map so the
// caller may mutate it safely; the map is never reused across events.
func (c Config) logEvent(Event string, fields map[string]any) {
	if c.Log == nil {
		return
	}
	c.Log(Event, fields)
}

// summarizeMessages reduces a message bundle to compact, log-safe JSON values:
// roles, per-message char counts, and image presence (size, not contents). The
// base64 image payload is deliberately omitted so logs stay small and secret.
func summarizeMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		entry := map[string]any{"role": string(m.Role)}
		var chars int
		var imgs int
		for _, p := range m.Content {
			chars += len(p.Text)
			if p.ImageB64 != "" {
				imgs++
			}
		}
		entry["chars"] = chars
		if imgs > 0 {
			entry["images"] = imgs
		}
		out = append(out, entry)
	}
	return out
}

// ellipsize limits a long value to a bounded preview with a length marker.
func ellipsize(s string, n int) string {
	s = fmt.Sprintf("%s", s)
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…(+%d)", len(s)-n)
}
