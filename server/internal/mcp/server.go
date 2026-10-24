// Package mcp serves the MCP 2024-11-05 HTTP+SSE transport used by Linx.
package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxSessions  = 128
	maxBodyBytes = 64 << 10
	maxRequests  = 1024
	queueSize    = 32
)

type Config struct{ Token, AgentID string }
type Identity struct{ DeviceID, AgentID, SessionID string }
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}
type Call func(context.Context, Identity, string, json.RawMessage) (ToolResult, error)

type Server struct {
	config   Config
	tools    []Tool
	call     Call
	mu       sync.Mutex
	sessions map[string]*session
}
type session struct {
	identity     Identity
	ctx          context.Context
	requests     chan request
	events       chan []byte
	initialized  bool
	cached       map[string]cachedReply
	activeMu     sync.Mutex
	activeID     string
	activeCancel context.CancelFunc
}
type cachedReply struct {
	hash [32]byte
	data []byte
}
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	invalid bool
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func New(config Config, tools []Tool, call Call) *Server {
	if tools == nil {
		tools = []Tool{}
	}
	return &Server{config: config, tools: tools, call: call, sessions: make(map[string]*session)}
}

func identityFrom(r *http.Request) Identity {
	return Identity{r.Header.Get("X-Linx-Device-Id"), r.Header.Get("X-Linx-Agent-Id"), r.Header.Get("X-Linx-Session-Id")}
}
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.config.Token == "" || s.config.AgentID == "" {
		http.Error(w, "MCP is not configured", http.StatusServiceUnavailable)
		return false
	}
	supplied := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(supplied), []byte(s.config.Token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// ServeSSE opens a transport session. Identity is trusted only from authenticated
// Linx headers; tools cannot select a different device through their arguments.
func (s *Server) ServeSSE(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	identity := identityFrom(r)
	if identity != (Identity{}) && (identity.DeviceID == "" || identity.SessionID == "" || identity.AgentID != s.config.AgentID) {
		http.Error(w, "invalid Linx identity", http.StatusForbidden)
		return
	}
	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "streaming unavailable", http.StatusInternalServerError)
		return
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		http.Error(w, "session unavailable", 500)
		return
	}
	id := base64.RawURLEncoding.EncodeToString(random)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	conn := &session{identity: identity, ctx: ctx, requests: make(chan request, queueSize), events: make(chan []byte, queueSize), cached: make(map[string]cachedReply)}
	s.mu.Lock()
	if len(s.sessions) >= maxSessions {
		s.mu.Unlock()
		http.Error(w, "too many sessions", http.StatusServiceUnavailable)
		return
	}
	s.sessions[id] = conn
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.sessions, id); s.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	write := func(event string, data []byte) error {
		_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return controller.Flush()
	}
	if err := write("endpoint", []byte("/api/v1/mcp/message?session_id="+id)); err != nil {
		return
	}
	go s.serve(conn)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-conn.events:
			if err := write("message", event); err != nil {
				return
			}
		case <-ticker.C:
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
	}
}

func (s *Server) ServeMessage(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	s.mu.Lock()
	conn := s.sessions[r.URL.Query().Get("session_id")]
	s.mu.Unlock()
	if conn == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if identityFrom(r) != conn.identity {
		http.Error(w, "Linx identity does not match session", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	var req request
	if !json.Valid(body) {
		req.invalid = true
	} else if json.Unmarshal(body, &req) != nil {
		req = request{}
	}
	if req.JSONRPC == "2.0" && len(req.ID) == 0 && req.Method == "notifications/cancelled" {
		var params struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		if json.Unmarshal(req.Params, &params) == nil && len(params.RequestID) > 0 && validID(params.RequestID) {
			conn.activeMu.Lock()
			if conn.activeID == string(params.RequestID) && conn.activeCancel != nil {
				conn.activeCancel()
			}
			conn.activeMu.Unlock()
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	select {
	case <-conn.ctx.Done():
		http.Error(w, "session closed", http.StatusGone)
	case conn.requests <- req:
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "session queue full", http.StatusTooManyRequests)
	}
}

func (s *Server) serve(conn *session) {
	for {
		select {
		case <-conn.ctx.Done():
			return
		case req := <-conn.requests:
			if conn.ctx.Err() != nil {
				return
			}
			data := s.handle(conn, req)
			if data == nil {
				continue
			}
			select {
			case conn.events <- data:
			case <-conn.ctx.Done():
				return
			}
		}
	}
}
func encodeReply(id json.RawMessage, result any, err *rpcError) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	data, _ := json.Marshal(response{JSONRPC: "2.0", ID: id, Result: result, Error: err})
	return data
}
func validID(id json.RawMessage) bool {
	if len(id) == 0 {
		return true
	}
	var value any
	if json.Unmarshal(id, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}
func (s *Server) handle(conn *session, req request) []byte {
	if req.invalid {
		return encodeReply(nil, nil, &rpcError{-32700, "Parse error"})
	}
	if req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
		id := req.ID
		if !validID(id) {
			id = nil
		}
		return encodeReply(id, nil, &rpcError{-32600, "Invalid Request"})
	}
	if len(req.ID) == 0 {
		return nil
	} // Includes notifications/initialized; notifications never receive replies.
	compact := bytes.NewBuffer(nil)
	_ = json.Compact(compact, req.Params)
	hash := sha256.Sum256(append([]byte(req.Method+"\x00"), compact.Bytes()...))
	key := string(req.ID)
	if previous, ok := conn.cached[key]; ok {
		if previous.hash != hash {
			return encodeReply(req.ID, nil, &rpcError{-32600, "Request ID reused with different content"})
		}
		return previous.data
	}
	// Never evict successful calls: replaying an old ID must not repeat a mutation.
	// A client opens a new transport after this bounded session request budget.
	if len(conn.cached) >= maxRequests {
		return encodeReply(req.ID, nil, &rpcError{-32000, "Session request limit reached; reconnect"})
	}
	result, rpcErr := s.dispatch(conn, req)
	data := encodeReply(req.ID, result, rpcErr)
	conn.cached[key] = cachedReply{hash: hash, data: data}
	return data
}
func (s *Server) dispatch(conn *session, req request) (any, *rpcError) {
	if req.Method == "initialize" {
		var params struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      json.RawMessage `json:"clientInfo"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" {
			return nil, &rpcError{-32602, "Invalid initialize parameters"}
		}
		conn.initialized = true
		return map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fleet-ptt", "version": "1.0.0"}}, nil
	}
	if req.Method == "ping" {
		return map[string]any{}, nil
	}
	if !conn.initialized {
		return nil, &rpcError{-32000, "Session is not initialized"}
	}
	switch req.Method {
	case "tools/list":
		return map[string]any{"tools": s.tools}, nil
	case "tools/call":
		if conn.identity == (Identity{}) {
			return nil, &rpcError{-32001, "Device identity required"}
		}
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
			Meta      json.RawMessage `json:"_meta,omitempty"`
		}
		decoder := json.NewDecoder(bytes.NewReader(req.Params))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&params) != nil || params.Name == "" {
			return nil, &rpcError{-32602, "Invalid tool parameters"}
		}
		known := false
		for _, tool := range s.tools {
			if tool.Name == params.Name {
				known = true
				break
			}
		}
		if !known {
			return nil, &rpcError{-32602, "Unknown tool"}
		}
		params.Arguments = bytes.TrimSpace(params.Arguments)
		if len(params.Arguments) == 0 {
			params.Arguments = json.RawMessage("{}")
		}
		if len(params.Arguments) == 0 || params.Arguments[0] != '{' {
			return nil, &rpcError{-32602, "Tool arguments must be an object"}
		}
		ctx, cancel := context.WithCancel(conn.ctx)
		conn.activeMu.Lock()
		conn.activeID, conn.activeCancel = string(req.ID), cancel
		conn.activeMu.Unlock()
		defer func() {
			cancel()
			conn.activeMu.Lock()
			conn.activeID, conn.activeCancel = "", nil
			conn.activeMu.Unlock()
		}()
		if err := ctx.Err(); err != nil {
			return nil, &rpcError{-32800, "Request cancelled"}
		}
		result, err := s.call(ctx, conn.identity, params.Name, params.Arguments)
		if ctx.Err() != nil {
			return nil, &rpcError{-32800, "Request cancelled"}
		}
		if err != nil {
			return nil, &rpcError{-32602, err.Error()}
		}
		if result.Content == nil {
			result.Content = []Content{}
		}
		return result, nil
	default:
		return nil, &rpcError{-32601, "Method not found"}
	}
}
