// Package mcp serves the MCP Streamable HTTP transport used by Linx.
package mcp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxSessions    = 128
	maxBodyBytes   = 64 << 10
	maxRequests    = 1024
	sessionTTL     = 30 * time.Minute
	toolTimeout    = 30 * time.Second
	latestProtocol = "2025-06-18"
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
	now      func() time.Time
}
type session struct {
	identity     Identity
	protocol     string
	ctx          context.Context
	cancel       context.CancelFunc
	lastUsed     time.Time // guarded by Server.mu
	lock         chan struct{}
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
	return &Server{config: config, tools: tools, call: call, sessions: make(map[string]*session), now: time.Now}
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

// ServeHTTP serves a single Streamable HTTP endpoint. Responses use JSON;
// server-initiated streams are not needed because device events use their own WS.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	// Only the authenticated Linx server calls this endpoint, never a browser.
	if _, present := r.Header["Origin"]; present {
		http.Error(w, "browser origins are not allowed", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	identity := identityFrom(r)
	if identity != (Identity{}) && (identity.DeviceID == "" || identity.SessionID == "" || identity.AgentID != s.config.AgentID) {
		http.Error(w, "invalid Linx identity", http.StatusForbidden)
		return
	}
	version := r.Header.Get("MCP-Protocol-Version")
	if version != "" && !supportedProtocol(version) {
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}
	id := r.Header.Get("Mcp-Session-Id")
	var conn *session
	s.mu.Lock()
	// debt: bounded O(maxSessions) idle cleanup; use an expiry heap if the
	// deployment outgrows the 128-session local testing limit.
	for key, candidate := range s.sessions {
		if s.now().Sub(candidate.lastUsed) >= sessionTTL {
			candidate.cancel()
			delete(s.sessions, key)
		}
	}
	if id != "" {
		conn = s.sessions[id]
		if conn != nil && conn.identity == identity {
			conn.lastUsed = s.now()
		}
	}
	s.mu.Unlock()
	if id != "" && conn == nil {
		http.Error(w, "unknown session", http.StatusNotFound)
		return
	}
	if conn != nil && identity != conn.identity {
		http.Error(w, "Linx identity does not match session", http.StatusForbidden)
		return
	}
	// With a session, the negotiated version is known even if the header is
	// omitted. Without a session, the initial request negotiates its version.
	if conn != nil && version != "" && version != conn.protocol {
		http.Error(w, "protocol version does not match session", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodDelete {
		if conn == nil {
			http.Error(w, "session ID required", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		delete(s.sessions, id)
		s.mu.Unlock()
		conn.cancel()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if !accepts(r.Header.Get("Accept"), "application/json") || !accepts(r.Header.Get("Accept"), "text/event-stream") {
		http.Error(w, "Accept must include application/json and text/event-stream", http.StatusNotAcceptable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	var req request
	if !json.Valid(body) {
		writeReply(w, http.StatusBadRequest, encodeReply(nil, nil, &rpcError{-32700, "Parse error"}))
		return
	}
	if json.Unmarshal(body, &req) != nil || req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
		writeReply(w, http.StatusBadRequest, encodeReply(nil, nil, &rpcError{-32600, "Invalid Request"}))
		return
	}
	if conn == nil {
		if req.Method != "initialize" || len(req.ID) == 0 {
			http.Error(w, "session ID required", http.StatusBadRequest)
			return
		}
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" {
			writeReply(w, http.StatusOK, encodeReply(req.ID, nil, &rpcError{-32602, "Invalid initialize parameters"}))
			return
		}
		protocol := params.ProtocolVersion
		if !supportedProtocol(protocol) {
			protocol = latestProtocol
		}
		random := make([]byte, 32)
		if _, err := rand.Read(random); err != nil {
			http.Error(w, "session unavailable", http.StatusInternalServerError)
			return
		}
		id = base64.RawURLEncoding.EncodeToString(random)
		ctx, cancel := context.WithCancel(context.Background())
		conn = &session{identity: identity, protocol: protocol, ctx: ctx, cancel: cancel, lastUsed: s.now(), lock: make(chan struct{}, 1), cached: make(map[string]cachedReply)}
		// Initialize before publishing so parallel requests cannot observe a
		// partially initialized transport session.
		data := s.handle(conn, req)
		s.mu.Lock()
		if len(s.sessions) >= maxSessions {
			s.mu.Unlock()
			cancel()
			http.Error(w, "too many sessions", http.StatusServiceUnavailable)
			return
		}
		s.sessions[id] = conn
		s.mu.Unlock()
		w.Header().Set("Mcp-Session-Id", id)
		writeReply(w, http.StatusOK, data)
		return
	}
	if len(req.ID) == 0 {
		if req.Method == "notifications/cancelled" {
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
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	select {
	case conn.lock <- struct{}{}:
		defer func() { <-conn.lock }()
	case <-conn.ctx.Done():
		http.Error(w, "session closed", http.StatusNotFound)
		return
	case <-r.Context().Done():
		return
	}
	if conn.ctx.Err() != nil {
		http.Error(w, "session closed", http.StatusNotFound)
		return
	}
	// A lost HTTP response does not mean the operation was cancelled. Finish
	// within toolTimeout and cache its reply so retrying the ID is safe.
	writeReply(w, http.StatusOK, s.handle(conn, req))
}
func supportedProtocol(version string) bool {
	return version == latestProtocol
}
func accepts(header, media string) bool {
	for _, value := range strings.Split(header, ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(value))
		if err == nil && kind == media {
			if q, exists := params["q"]; exists {
				quality, err := strconv.ParseFloat(q, 64)
				if err != nil || quality <= 0 || quality > 1 {
					continue
				}
			}
			return true
		}
	}
	return false
}
func writeReply(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
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
		if conn.initialized {
			return nil, &rpcError{-32600, "Session is already initialized"}
		}
		var params struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Capabilities    json.RawMessage `json:"capabilities"`
			ClientInfo      json.RawMessage `json:"clientInfo"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ProtocolVersion == "" {
			return nil, &rpcError{-32602, "Invalid initialize parameters"}
		}
		conn.initialized = true
		return map[string]any{"protocolVersion": conn.protocol, "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "fleet-ptt", "version": "1.0.0"}}, nil
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
		ctx, cancel := context.WithTimeout(conn.ctx, toolTimeout)
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
