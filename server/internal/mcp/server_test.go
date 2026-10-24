package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type client struct {
	t         *testing.T
	server    *httptest.Server
	transport *Server
	endpoint  string
	identity  Identity
	sessionID string
}

func start(t *testing.T, identity Identity, call Call) *client {
	t.Helper()
	transport := New(Config{Token: "secret", AgentID: "agent"}, []Tool{{Name: "create_channel", Description: "Create", InputSchema: map[string]any{"type": "object"}}}, call)
	server := httptest.NewServer(transport)
	t.Cleanup(server.Close)
	return &client{t: t, server: server, transport: transport, endpoint: "/api/v1/mcp", identity: identity}
}
func addHeaders(req *http.Request, identity Identity) {
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("X-Linx-Device-Id", identity.DeviceID)
	req.Header.Set("X-Linx-Agent-Id", identity.AgentID)
	req.Header.Set("X-Linx-Session-Id", identity.SessionID)
}
func (c *client) request(method, body string, identity Identity) *http.Request {
	req, _ := http.NewRequest(method, c.server.URL+c.endpoint, strings.NewReader(body))
	addHeaders(req, identity)
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	return req
}
func (c *client) send(req *http.Request) (int, []byte, http.Header) {
	c.t.Helper()
	response, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return response.StatusCode, body, response.Header
}
func (c *client) post(body string, identity Identity) int {
	status, _, _ := c.send(c.request("POST", body, identity))
	return status
}
func (c *client) rpc(body string) map[string]any {
	c.t.Helper()
	status, data, headers := c.send(c.request("POST", body, c.identity))
	if status != 200 {
		c.t.Fatalf("POST status %d: %s", status, data)
	}
	if headers.Get("Content-Type") != "application/json" {
		c.t.Fatal(headers)
	}
	if id := headers.Get("Mcp-Session-Id"); id != "" {
		c.sessionID = id
	}
	var reply map[string]any
	if err := json.Unmarshal(data, &reply); err != nil {
		c.t.Fatal(err)
	}
	return reply
}
func (c *client) initialize() {
	c.t.Helper()
	reply := c.rpc(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	if reply["error"] != nil || c.sessionID == "" {
		c.t.Fatalf("initialize: %v", reply)
	}
	if reply["result"].(map[string]any)["protocolVersion"] != latestProtocol {
		c.t.Fatal(reply)
	}
	if status := c.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, c.identity); status != 202 {
		c.t.Fatal(status)
	}
}
func errorCode(t *testing.T, reply map[string]any, want int) {
	t.Helper()
	e, ok := reply["error"].(map[string]any)
	if !ok || e["code"] != float64(want) {
		t.Fatalf("wanted error %d: %v", want, reply)
	}
}
func TestTransportToolsAndReplay(t *testing.T) {
	var calls atomic.Int32
	identity := Identity{"mac", "agent", "voice-session"}
	c := start(t, identity, func(ctx context.Context, got Identity, name string, args json.RawMessage) (ToolResult, error) {
		calls.Add(1)
		if got != identity || name != "create_channel" || string(args) != `{"name":"车队"}` {
			t.Errorf("unexpected call %v %s %s", got, name, args)
		}
		return ToolResult{Content: []Content{{Type: "text", Text: "created"}}}, nil
	})
	c.initialize()
	list := c.rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	result := list["result"].(map[string]any)
	if len(result["tools"].([]any)) != 1 {
		t.Fatal(result)
	}
	body := `{"jsonrpc":"2.0","id":"action-1","method":"tools/call","params":{"name":"create_channel","arguments":{"name":"车队"}}}`
	first := c.rpc(body)
	second := c.rpc(body)
	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) || calls.Load() != 1 {
		t.Fatalf("replayed side effect: calls=%d first=%s second=%s", calls.Load(), a, b)
	}
	errorCode(t, c.rpc(strings.Replace(body, "车队", "other", 1)), -32600)
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`), -32602)
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_channel","arguments":[],"device_id":"other"}}`), -32602)
}
func TestTransportIdentityAndAuthentication(t *testing.T) {
	c := start(t, Identity{"mac", "agent", "voice"}, func(context.Context, Identity, string, json.RawMessage) (ToolResult, error) {
		t.Error("unexpected call")
		return ToolResult{}, nil
	})
	c.initialize()
	req, _ := http.NewRequest("POST", c.server.URL+c.endpoint, strings.NewReader(`{}`))
	response, err := c.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.StatusCode)
	}
	if status := c.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, Identity{"other", "agent", "voice"}); status != 403 {
		t.Fatal(status)
	}
	if status := c.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`, Identity{}); status != 403 {
		t.Fatal(status)
	}
	for _, identity := range []Identity{{"mac", "wrong", "voice"}, {"mac", "agent", ""}} {
		req, _ := http.NewRequest("POST", c.server.URL+"/api/v1/mcp", nil)
		addHeaders(req, identity)
		response, err := c.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal(response.StatusCode)
		}
	}
}
func TestDiscoveryCannotCallTools(t *testing.T) {
	c := start(t, Identity{}, func(context.Context, Identity, string, json.RawMessage) (ToolResult, error) {
		t.Error("unexpected call")
		return ToolResult{}, nil
	})
	if status := c.post(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, c.identity); status != 400 {
		t.Fatal(status)
	}
	c.initialize()
	if list := c.rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); list["error"] != nil {
		t.Fatal(list)
	}
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_channel"}}`), -32001)
	for _, body := range []string{`not json`, `{"jsonrpc":"2.0","id":{},"method":"ping"}`, `[]`, `null`} {
		if status := c.post(body, c.identity); status != 400 {
			t.Fatal(status)
		}
	}
}
func TestDisconnectDoesNotCancelTool(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c := start(t, Identity{"mac", "agent", "voice"}, func(ctx context.Context, _ Identity, _ string, _ json.RawMessage) (ToolResult, error) {
		calls.Add(1)
		close(started)
		select {
		case <-finish:
			return ToolResult{}, nil
		case <-ctx.Done():
			t.Error("disconnect cancelled tool")
			return ToolResult{}, ctx.Err()
		}
	})
	c.initialize()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_channel"}}`
	ctx, cancel := context.WithCancel(context.Background())
	req := c.request("POST", body, c.identity).WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		response, _ := c.server.Client().Do(req)
		if response != nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool not started")
	}
	cancel()
	<-done
	close(finish)
	if reply := c.rpc(body); reply["error"] != nil {
		t.Fatal(reply)
	}
	if calls.Load() != 1 {
		t.Fatal("retry repeated mutation")
	}
}
func TestSessionRequestBudgetDoesNotEvictCalls(t *testing.T) {
	var calls int
	server := New(Config{}, []Tool{{Name: "mutate"}}, func(context.Context, Identity, string, json.RawMessage) (ToolResult, error) {
		calls++
		return ToolResult{}, nil
	})
	conn := &session{ctx: context.Background(), identity: Identity{"mac", "agent", "voice"}, initialized: true, cached: make(map[string]cachedReply)}
	original := request{JSONRPC: "2.0", ID: json.RawMessage(`"first"`), Method: "tools/call", Params: json.RawMessage(`{"name":"mutate"}`)}
	first := server.handle(conn, original)
	for i := 1; i < maxRequests; i++ {
		id, _ := json.Marshal(i)
		server.handle(conn, request{JSONRPC: "2.0", ID: id, Method: "ping"})
	}
	reply := server.handle(conn, request{JSONRPC: "2.0", ID: json.RawMessage(`"new"`), Method: "tools/call", Params: original.Params})
	if !strings.Contains(string(reply), "Session request limit") {
		t.Fatalf("unbounded session %s", reply)
	}
	if replay := server.handle(conn, original); string(replay) != string(first) || calls != 1 {
		t.Fatalf("duplicate mutation: calls=%d", calls)
	}
}

func TestToolCancellationAndDelete(t *testing.T) {
	for _, action := range []string{"notification", "delete"} {
		t.Run(action, func(t *testing.T) {
			started := make(chan struct{})
			c := start(t, Identity{"mac", "agent", "voice"}, func(ctx context.Context, _ Identity, _ string, _ json.RawMessage) (ToolResult, error) {
				close(started)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			})
			c.initialize()
			done := make(chan map[string]any, 1)
			go func() {
				done <- c.rpc(`{"jsonrpc":"2.0","id":"cancel-me","method":"tools/call","params":{"name":"create_channel","arguments":{},"_meta":{"progressToken":"p"}}}`)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("tool not started")
			}
			if action == "notification" {
				if status := c.post(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"cancel-me"}}`, c.identity); status != 202 {
					t.Fatal(status)
				}
			} else {
				status, _, _ := c.send(c.request("DELETE", "", c.identity))
				if status != 204 {
					t.Fatal(status)
				}
				if status := c.post(`{"jsonrpc":"2.0","id":4,"method":"ping"}`, c.identity); status != 404 {
					t.Fatal(status)
				}
			}
			select {
			case reply := <-done:
				errorCode(t, reply, -32800)
			case <-time.After(time.Second):
				t.Fatal("cancellation blocked")
			}
		})
	}
}

func TestHTTPContract(t *testing.T) {
	c := start(t, Identity{}, nil)
	c.initialize()
	for _, tc := range []struct {
		name, method, header, value string
		status                      int
	}{
		{"GET", "GET", "", "", 405},
		{"missing bearer", "POST", "Authorization", "", 401},
		{"browser origin", "POST", "Origin", "https://evil.example", 403},
		{"content type", "POST", "Content-Type", "text/plain", 415},
		{"accept", "POST", "Accept", "application/json", 406},
		{"unknown version", "POST", "MCP-Protocol-Version", "2099-01-01", 400},
		{"older version", "POST", "MCP-Protocol-Version", "2025-03-26", 400},
		{"unknown session", "POST", "Mcp-Session-Id", "missing", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := c.request(tc.method, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, c.identity)
			if tc.header != "" {
				req.Header.Set(tc.header, tc.value)
			}
			status, _, _ := c.send(req)
			if status != tc.status {
				t.Fatalf("got %d want %d", status, tc.status)
			}
		})
	}
}

func TestNegotiationAndSessionExpiry(t *testing.T) {
	for _, version := range []string{"2025-06-18", "2025-03-26", "2024-11-05"} {
		t.Run(version, func(t *testing.T) {
			c := start(t, Identity{}, nil)
			reply := c.rpc(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version + `"}}`)
			if reply["result"].(map[string]any)["protocolVersion"] != latestProtocol {
				t.Fatal(reply)
			}
			c.transport.mu.Lock()
			conn := c.transport.sessions[c.sessionID]
			conn.lastUsed = time.Now().Add(-sessionTTL)
			c.transport.mu.Unlock()
			if status := c.post(`{"jsonrpc":"2.0","id":2,"method":"ping"}`, c.identity); status != 404 {
				t.Fatal(status)
			}
			if conn.ctx.Err() == nil {
				t.Fatal("expired session context remains active")
			}
			c.sessionID = ""
			c.initialize()
		})
	}
}

func TestConcurrentReplayRunsToolOnce(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	c := start(t, Identity{"mac", "agent", "voice"}, func(context.Context, Identity, string, json.RawMessage) (ToolResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-finish
		return ToolResult{Content: []Content{{Type: "text", Text: "done"}}}, nil
	})
	c.initialize()
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_channel"}}`
	results := make(chan map[string]any, 2)
	go func() { results <- c.rpc(body) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool not started")
	}
	go func() { results <- c.rpc(body) }()
	close(finish)
	for i := 0; i < 2; i++ {
		select {
		case reply := <-results:
			if reply["error"] != nil {
				t.Fatal(reply)
			}
		case <-time.After(time.Second):
			t.Fatal("replay blocked")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("concurrent replay executed %d times", calls.Load())
	}
}

func TestSessionCapacityAndMalformedInitialize(t *testing.T) {
	c := start(t, Identity{}, nil)
	bad := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`
	errorCode(t, c.rpc(bad), -32602)
	if c.sessionID != "" || len(c.transport.sessions) != 0 {
		t.Fatal("invalid initialize allocated a session")
	}
	for i := 0; i < maxSessions; i++ {
		c.sessionID = ""
		c.initialize()
	}
	lastID := c.sessionID
	c.sessionID = ""
	body := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`
	if status := c.post(body, c.identity); status != 503 {
		t.Fatalf("capacity status %d", status)
	}
	c.sessionID = lastID
	status, _, _ := c.send(c.request("DELETE", "", c.identity))
	if status != 204 {
		t.Fatal(status)
	}
	c.sessionID = ""
	c.initialize()
}
