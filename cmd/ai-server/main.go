// Command ai-server is an HTTP service that accepts a geometry/Math problem
// (as text or an inline image) and returns GeoGebra teaching instructions,
// generated via an OpenAI-compatible chat backend and gated through the ggcm
// validator with a bounded auto-repair loop.
//
// Usage:
//
//	GGCM_AI_ENDPOINT=... GGCM_AI_MODEL=... GGCM_AI_API_KEY=... \
//	   go run ./cmd/ai-server -addr :8080
//
// Endpoints:
//
//	GET  /api/health            liveness probe
//	POST /api/chat              generate instructions for a problem
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hycjack/geogebra-dsl-go/internal/ai"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	cfg := ai.LoadConfig()
	client := ai.NewClient(cfg)
	sessions := newSessionStore(cfg.MaxHistory)

	mux := http.NewServeMux()
	server := &server{cfg: cfg, client: client, sessions: sessions}
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/chat", server.handleChat)

	log.Printf("ai-server listening on %s (model=%s endpoint=%s)", *addr, cfg.Model, cfg.Endpoint)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

type server struct {
	cfg      ai.Config
	client   ai.ChatClient
	sessions *sessionStore
}

// chatResponse wraps the generation result with the resolved session id so the
// client can attach subsequent "append" turns to the same conversation.
type chatResponse struct {
	SessionID string `json:"session_id"`
	*ai.Result
}

// chatRequest mirrors DESIGN-AI.md §7.2.
type chatRequest struct {
	SessionID string `json:"session_id"`
	InputType string `json:"input_type"`           // "text" | "image"
	Text      string `json:"text"`                 // problem statement (text) or guide (image)
	ImageB64  string `json:"image_b64,omitempty"`  // base64 image payload (image input)
	ImageMIME string `json:"image_mime,omitempty"` // e.g. image/png
	Stream    bool   `json:"stream,omitempty"`     // SSE response
	Append    string `json:"append,omitempty"`     // optional follow-up instruction against prior result
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}

	sess := s.sessions.Get(req.SessionID)
	sysMsg := ai.SystemMessage()

	// A follow-up "append" instruction modifies the dialog's prior result; it can
	// arrive without a fresh problem `text`, so it gets its own validation and
	// does not require a non-empty text field.
	var userMsg ai.Message
	if strings.TrimSpace(req.Append) != "" {
		combined := "对上一版结果做如下追加修改：\n" + req.Append
		if req.InputType == "text" && strings.TrimSpace(req.Text) != "" {
			combined = "原始题目：\n" + req.Text + "\n\n" + combined
		}
		userMsg = ai.TextUserMessage(combined)
	} else {
		usrMsg, err := buildUserMessage(s.cfg, req)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		userMsg = *usrMsg
	}
	sess.Append(userMsg)

	res := ai.Generate(r.Context(), s.client, s.cfg, ai.GenerateRequest{
		Session:   sess,
		SystemMsg: sysMsg,
		UserMsg:   userMsg,
	})

	envelope := &chatResponse{SessionID: sess.ID}
	envelope.Result = res
	if req.Stream {
		s.streamResult(w, envelope)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(envelope)
}

// buildUserMessage validates the request and builds the user turn, preferring
// multimodal image when requested (with vision disabled it is refused).
func buildUserMessage(cfg ai.Config, req chatRequest) (*ai.Message, error) {
	switch req.InputType {
	case "text":
		if strings.TrimSpace(req.Text) == "" {
			return nil, fmt.Errorf("input_type=text requires a non-empty 'text' field")
		}
		m := ai.TextUserMessage(req.Text)
		return &m, nil
	case "image":
		if req.ImageB64 == "" {
			return nil, fmt.Errorf("input_type=image requires a base64 'image_b64' field")
		}
		if cfg.DisableVision {
			return nil, fmt.Errorf("visual input is disabled on the server; send the problem as text instead")
		}
		if len(req.ImageB64) > cfg.MaxImageBytes {
			return nil, fmt.Errorf("image too large (base64 %d bytes, limit %d)", len(req.ImageB64), cfg.MaxImageBytes)
		}
		m := ai.ImageUserMessage(req.ImageB64, req.ImageMIME, req.Text)
		return &m, nil
	default:
		return nil, fmt.Errorf("input_type must be 'text' or 'image'")
	}
}

// streamResult emits the final (enveloped: result + session id) response as a
// single SSE "result" event.
func (s *server) streamResult(w http.ResponseWriter, env *chatResponse) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	data, _ := json.Marshal(env)
	fmt.Fprintf(w, "event: result\ndata: %s\n\n", data)
	fl.Flush()
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sessionStore keeps in-memory sessions, creating one on first use.
type sessionStore struct {
	max      int
	sessions map[string]*ai.Session
}

func newSessionStore(max int) *sessionStore {
	return &sessionStore{max: max, sessions: map[string]*ai.Session{}}
}

func (ss *sessionStore) Get(id string) *ai.Session {
	if id == "" {
		id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	if s, ok := ss.sessions[id]; ok {
		return s
	}
	s := ai.NewSession(id, ss.max)
	ss.sessions[id] = s
	return s
}
