// Command ai-server is an HTTP service that accepts a geometry/Math problem
// (as text or an inline image) and returns GeoGebra teaching instructions,
// generated via an OpenAI-compatible chat backend and gated through the ggbcheck
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
//	GET  /api/config            expose run-time endpoint/model/vision config
//	GET  /                      embedded web UI (GeoGebra canvas + AI chat)
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hycjack/geogebra-dsl-go/internal/ai"
)

//go:embed ui.html
var uiHTML []byte

// staticFS serves the local GeoGebra loader (and any future web assets) so the
// render engine can be loaded same-origin instead of depending on a CDN.
//
//go:embed static
var staticFS embed.FS

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	logPath := flag.String("log", "", "path to write structured logs (JSON lines); empty writes to stdout")
	flag.Parse()

	cfg := ai.LoadConfig()

	// Wire the structured logger into the config BEFORE building the default
	// client, so the default client's per-call llm.request/llm.response/llm.retry
	// events are emitted too (NewClient copies Config, so Log must already be set).
	lj := newLogJSON(os.Stdout)
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			log.Fatalf("open log file %s: %v", *logPath, err)
		}
		lj.w = f
	}
	cfg.Log = lj.emit
	client := ai.NewClient(cfg)

	sessions := newSessionStore(cfg.MaxHistory)

	mux := http.NewServeMux()
	server := &server{cfg: cfg, client: client, sessions: sessions, logs: lj}
	mux.HandleFunc("/", server.handleUI)
	mux.Handle("/static/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/deployggb.js", server.handleDeployGGB)
	mux.HandleFunc("/api/health", server.handleHealth)
	mux.HandleFunc("/api/config", server.handleConfig)
	mux.HandleFunc("/api/logs", server.handleLogs)
	mux.HandleFunc("/api/chat", server.handleChat)

	log.Printf("ai-server listening on %s (model=%s endpoint=%s)", *addr, cfg.Model, cfg.Endpoint)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

// logJSON is a concurrency-safe JSON-lines writer that also fans each event out
// to live web subscribers (the browser's log panel via SSE /api/logs), so per-call
// LLM parameters and returned content are visible in the UI, not just the file.
type logJSON struct {
	mu      *sync.Mutex
	w       io.Writer
	pending []json.RawMessage // ring of the last logMax events for late subscribers
	subs    map[chan json.RawMessage]struct{}
}

// logMax is how many recent events a newly-connected subscriber replays so the
// UI shows history, not just events since the browser connected.
const logMax = 500

func newLogJSON(w io.Writer) *logJSON {
	return &logJSON{mu: new(sync.Mutex), w: w, subs: map[chan json.RawMessage]struct{}{}}
}

// Subscribe registers a live-event receiver. It immediately replays the last
// buffered events (in order) then streams new ones until the returned cancel is
// called or the channel is closed. Returned channel receives json.RawMessage of
// one record per event. The replay is best-effort and non-blocking: a backlog
// larger than the channel's capacity drops the oldest already-buffered events
// rather than stalling the caller (and, critically, the lock in emit) behind a
// subscriber that has not started draining yet.
func (l *logJSON) Subscribe() (chan json.RawMessage, func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ch := make(chan json.RawMessage, 256)
	for _, rec := range l.pending {
		select {
		case ch <- rec:
		default: // channel full while replaying: drop rather than block under lock
		}
	}
	l.subs[ch] = struct{}{}
	cancel := func() {
		l.mu.Lock()
		delete(l.subs, ch)
		l.mu.Unlock()
	}
	return ch, cancel
}

// buffer appends a record to the rolling ring for late subscribers.
func (l *logJSON) buffer(rec json.RawMessage) {
	l.pending = append(l.pending, rec)
	if len(l.pending) > logMax {
		l.pending = l.pending[len(l.pending)-logMax:]
	}
}

// fanout forwards a record to every live subscriber non-blockingly (a slow or
// disconnected subscriber is dropped rather than stalling generation). Callers
// must hold l.mu.
func (l *logJSON) fanout(rec json.RawMessage) {
	for ch := range l.subs {
		select {
		case ch <- rec:
		default: // subscriber not keeping up; drop rather than block
		}
	}
}

// emit is an ai.LogFunc that serializes the event+fields to one JSON line and
// fans it out to live web log subscribers. It is invoked synchronously by the
// ai package across potentially concurrent requests, so it holds the lock.
func (l *logJSON) emit(Event string, fields map[string]any) {
	rec := map[string]any{"time": time.Now().UTC().Format(time.RFC3339Nano), "event": Event}
	for k, v := range fields {
		rec[k] = v
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, string(b)+"\n")
	l.buffer(b)
	l.fanout(b)
}

type server struct {
	cfg      ai.Config
	client   ai.ChatClient
	sessions *sessionStore
	logs     *logJSON
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
	Mode      string `json:"mode,omitempty"`       // requested view: "2d"/"classic"/"geometry" or "3d"
	// Endpoint, Model and APIKey are OPTIONAL per-request overrides chosen in
	// the web UI. They replace the server defaults for this call only. When a
	// browser supplies an api_key it is used for that request and never
	// persisted server-side; if omitted the server's environment key is used.
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// effectiveConfig returns the config used for one request: the server defaults
// with any per-request endpoint/model/key overrides applied, and a guard against
// credential leakage.
//
// Security rule: a per-request `endpoint` override (a foreign/base URL) must be
// paired with the caller's OWN `api_key`. The server's environment key must
// never be forwarded to an arbitrary, caller-controlled host — that would leak
// the server credential to any URL. Mirroring, a caller-supplied endpoint is
// also validated to be an http(s) URL before it is accepted. The single
// argument is a Config, and overrides are read from it.
func (s *server) effectiveConfig(cfg ai.Config, endpoint, model, apiKey string) (ai.Config, error) {
	endpoint = strings.TrimSpace(endpoint)
	model = strings.TrimSpace(model)
	apiKey = strings.TrimSpace(apiKey)

	switch {
	case endpoint != "" && apiKey == "":
		// A custom endpoint with no explicit key: the server must not attach its
		// own env key to a host it does not control. Refuse rather than leak.
		return cfg, fmt.Errorf("a custom endpoint requires its own 'api_key'; the server will not send its key to a third-party endpoint")
	case endpoint != "":
		if err := validEndpointURL(endpoint); err != nil {
			return cfg, err
		}
		cfg.Endpoint = endpoint
		cfg.APIKey = apiKey
	case apiKey != "":
		// Same endpoint as the server, but the caller supplies its own key for
		// this call only.
		cfg.APIKey = apiKey
	}
	if model != "" {
		cfg.Model = model
	}
	return cfg, nil
}

// validEndpointURL reports whether endpoint is an http(s) base URL with a host
// and a scheme we will actually forward requests to. Rejects file:, data:,
// ftp: and other non-HTTP schemes so the client can never be steered to a
// non-HTTP transport.
func validEndpointURL(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid endpoint URL: %q", endpoint)
	}
	switch u.Scheme {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("endpoint URL must use http or https, got %q", u.Scheme)
	}
}

func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// handleConfig exposes the server's run-time model configuration to the UI so
// the config panel can prefill. The API key's VALUE is deliberately omitted;
// only a boolean indicates whether one is set server-side.
func (s *server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"endpoint":        s.cfg.Endpoint,
		"model":           s.cfg.Model,
		"vision_enabled":  !s.cfg.DisableVision,
		"max_image_bytes": s.cfg.MaxImageBytes,
		"has_api_key":     strings.TrimSpace(s.cfg.APIKey) != "",
	})
}

// handleLogs streams the structured service log (llm.request / llm.response /
// llm.retry / generate.* / http.*) to the browser log panel over Server-Sent
// Events. Each record is one JSON line, echoed as `event: log`. The connection
// stays open until the client disconnects or the context is canceled.
func (s *server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if s.logs == nil {
		writeError(w, http.StatusInternalServerError, "logs disabled")
		return
	}
	ch, cancel := s.logs.Subscribe()
	defer cancel()

	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Commit the 200 + headers immediately. Go's HTTP server buffers the response
	// and only sends headers when the handler returns or flushes; without this the
	// client never sees the SSE response header and the connection handshake hangs.
	fl.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case rec, open := <-ch:
			if !open {
				return
			}
			fmt.Fprintf(w, "event: log\ndata: %s\n\n", rec)
			fl.Flush()
		}
	}
}

// handleUI serves the embedded single-page web UI at "/". Anything that isn't
// one of the /api/* endpoints (including "/", "/ui", favicon) is served as the
// app shell, so a browser loading the server root lands on the GeoGebra + AI UI.
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/ui" && r.URL.Path != "/favicon.ico" {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/favicon.ico" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(uiHTML)
}

// handleDeployGGB serves the local GeoGebra loader at a stable path the UI
// references first, so rendering does not depend on a CDN being reachable.
func (s *server) handleDeployGGB(w http.ResponseWriter, r *http.Request) {
	b, err := staticFS.ReadFile("static/deployggb.js")
	if err != nil {
		http.Error(w, "static asset missing", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Write(b)
}

func (s *server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	// Bound the whole request body up front. The base64 image is decoded into
	// memory here, so without a cap an oversized body would be fully buffered
	// before buildUserMessage's MaxImageBytes check runs. The cap is the image
	// byte limit plus generous headroom for the JSON envelope + text fields.
	body := http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxImageBytes)+64<<10)
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		s.logJSON("http.request", map[string]any{"session_id": req.SessionID, "error": "invalid_json"})
		writeError(w, 400, "invalid JSON request body")
		return
	}

	sess := s.sessions.Get(req.SessionID)
	// View mode ("2d"/"classic"/"geometry"/"3d") steers the system prompt so the
	// model emits a 2D or 3D construction. Defaults to the 2D prompt.
	sysMsg := ai.SystemMessageFor(req.Mode)

	// Attach the shared logger (already set on s.cfg) to the effective config so
	// every LLM/script event in this request carries the resolved params.
	s.logJSON("http.request", map[string]any{
		"session_id": sess.ID,
		"input_type": req.InputType,
		"stream":     req.Stream,
		"mode":       req.Mode,
		"text_chars": len(req.Text),
		"image_b64":  len(req.ImageB64),
		"append":     req.Append != "",
		"endpoint":   strings.TrimSpace(req.Endpoint),
		"model":      strings.TrimSpace(req.Model),
	})

	// Apply per-request endpoint/model/key overrides (from the UI panel). A
	// fresh client carries the resolved config for this call only. The guard in
	// effectiveConfig refuses a custom endpoint without an explicit key so the
	// server's own credential is never forwarded to a caller-chosen host.
	effCfg, err := s.effectiveConfig(s.cfg, req.Endpoint, req.Model, req.APIKey)
	if err != nil {
		s.logJSON("http.request", map[string]any{"session_id": sess.ID, "error": "invalid_overrides: " + err.Error()})
		writeError(w, 400, err.Error())
		return
	}
	effCfg.Log = s.cfg.Log // the shared logger is not part of the request overrides
	effClient := s.client
	if strings.TrimSpace(req.Endpoint) != "" || strings.TrimSpace(req.Model) != "" || strings.TrimSpace(req.APIKey) != "" {
		effClient = ai.NewClient(effCfg)
	}

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
		usrMsg, err := buildUserMessage(effCfg, req)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		userMsg = *usrMsg
	}

	res := ai.Generate(r.Context(), effClient, effCfg, ai.GenerateRequest{
		Session:   sess,
		SystemMsg: sysMsg,
		UserMsg:   userMsg,
	})

	// Persist a completed turn into history so a subsequent "append"/follow-up
	// can build on the PREVIOUS final script instead of regenerating. History
	// only ever holds completed (user→assistant) rounds; the current userMsg is
	// fed via GenerateRequest.UserMsg, not pre-appended, to avoid duplication.
	// On failure we still store the last assistant result (possibly an empty
	// script) so the user→assistant pairing stays intact across all turns.
	sess.Append(userMsg)
	sess.Append(ai.TextAssistantMessage(res.Script))

	envelope := &chatResponse{SessionID: sess.ID}
	envelope.Result = res
	s.logJSON("http.result", map[string]any{
		"session_id":     sess.ID,
		"ok":             res.OK,
		"attempts":       res.Attempts,
		"executable":     res.Executable,
		"diagnostics":    res.Diagnostics,
		"script_chars":   len(res.Script),
		"script_preview": aiEllipsize(res.Script, 300),
		"teaching_note":  res.TeachingNote,
	})
	if req.Stream {
		s.streamResult(w, envelope)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(envelope)
}

// logJSON decorates a log event with an optional session id for correlation.
func (s *server) logJSON(Event string, fields map[string]any) {
	if s.logs == nil {
		return
	}
	s.logs.emit(Event, fields)
}

// aiEllipsize previews a long script for logs.
func aiEllipsize(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + fmt.Sprintf("…(+%d)", len(s)-n)
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

// sessionStore keeps a bounded set of in-memory sessions, creating one on first
// use. It is safe for concurrent use by the HTTP handler.
//
// The map is bounded: session ids are client-controlled, so without a cap an
// attacker could fill memory by sending many distinct ids. When the map reaches
// maxSessions the oldest-created session (smallest Unix-nano id, i.e. the
// earliest) is evicted to make room. This bounds memory while keeping active
// conversations intact.
type sessionStore struct {
	mu       sync.Mutex
	max      int
	sessions map[string]*ai.Session
}

const maxSessions = 1000

func newSessionStore(max int) *sessionStore {
	return &sessionStore{max: max, sessions: map[string]*ai.Session{}}
}

// Get returns the session for id (creating one if absent). An empty id is
// assigned a fresh one-name unique id.
func (ss *sessionStore) Get(id string) *ai.Session {
	if id == "" {
		id = fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s, ok := ss.sessions[id]; ok {
		return s
	}
	if len(ss.sessions) >= maxSessions {
		// Evict the earliest-created session to keep the map bounded. Session ids
		// use an increasing Unix-nano timestamp, so the lexicographically smallest
		// id is the oldest.
		var oldest string
		for k := range ss.sessions {
			if oldest == "" || k < oldest {
				oldest = k
			}
		}
		if oldest != "" {
			delete(ss.sessions, oldest)
		}
	}
	s := ai.NewSession(id, ss.max)
	ss.sessions[id] = s
	return s
}
