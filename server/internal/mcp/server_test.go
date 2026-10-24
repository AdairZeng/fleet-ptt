package mcp

import (
	"bufio"
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
	t        *testing.T
	server   *httptest.Server
	response *http.Response
	reader   *bufio.Reader
	endpoint string
	identity Identity
}

func start(t *testing.T, identity Identity, call Call) *client {
	t.Helper()
	transport := New(Config{Token: "secret", AgentID: "agent"}, []Tool{{Name: "create_channel", Description: "Create", InputSchema: map[string]any{"type": "object"}}}, call)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/mcp", transport.ServeSSE)
	mux.HandleFunc("POST /api/v1/mcp/message", transport.ServeMessage)
	server := httptest.NewServer(mux)
	req, _ := http.NewRequest("GET", server.URL+"/api/v1/mcp", nil)
	addHeaders(req, identity)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("GET status %d", response.StatusCode)
	}
	c := &client{t: t, server: server, response: response, reader: bufio.NewReader(response.Body), identity: identity}
	event, data := c.read()
	if event != "endpoint" {
		t.Fatalf("first event %q", event)
	}
	c.endpoint = data
	t.Cleanup(func() { response.Body.Close(); server.Close() })
	return c
}
func addHeaders(req *http.Request, identity Identity) {
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-Linx-Device-Id", identity.DeviceID)
	req.Header.Set("X-Linx-Agent-Id", identity.AgentID)
	req.Header.Set("X-Linx-Session-Id", identity.SessionID)
}
func (c *client) read() (string, string) {
	c.t.Helper()
	type result struct {
		event, data string
		err         error
	}
	done := make(chan result, 1)
	go func() {
		var event, data string
		for {
			line, err := c.reader.ReadString('\n')
			if err != nil {
				done <- result{err: err}
				return
			}
			line = strings.TrimSuffix(line, "\n")
			if line == "" && event != "" {
				done <- result{event: event, data: data}
				return
			}
			if strings.HasPrefix(line, "event: ") {
				event = strings.TrimPrefix(line, "event: ")
			}
			if strings.HasPrefix(line, "data: ") {
				data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			c.t.Fatal(r.err)
		}
		return r.event, r.data
	case <-time.After(3 * time.Second):
		c.t.Fatal("SSE response timed out")
		return "", ""
	}
}
func (c *client) post(body string, identity Identity) int {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.server.URL+c.endpoint, strings.NewReader(body))
	addHeaders(req, identity)
	response, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer response.Body.Close()
	io.Copy(io.Discard, response.Body)
	return response.StatusCode
}
func (c *client) rpc(body string) map[string]any {
	c.t.Helper()
	if status := c.post(body, c.identity); status != 202 {
		c.t.Fatalf("POST status %d", status)
	}
	event, data := c.read()
	if event != "message" {
		c.t.Fatalf("event %q", event)
	}
	var reply map[string]any
	if err := json.Unmarshal([]byte(data), &reply); err != nil {
		c.t.Fatal(err)
	}
	return reply
}
func (c *client) initialize() {
	c.t.Helper()
	reply := c.rpc(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	if reply["error"] != nil {
		c.t.Fatalf("initialize: %v", reply)
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
		req, _ := http.NewRequest("GET", c.server.URL+"/api/v1/mcp", nil)
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
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`), -32000)
	c.initialize()
	if list := c.rpc(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); list["error"] != nil {
		t.Fatal(list)
	}
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_channel"}}`), -32001)
	errorCode(t, c.rpc(`not json`), -32700)
	errorCode(t, c.rpc(`{"jsonrpc":"2.0","id":{},"method":"ping"}`), -32600)
}
func TestDisconnectCancelsTool(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	c := start(t, Identity{"mac", "agent", "voice"}, func(ctx context.Context, _ Identity, _ string, _ json.RawMessage) (ToolResult, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return ToolResult{}, ctx.Err()
	})
	c.initialize()
	c.post(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"create_channel"}}`, c.identity)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool not started")
	}
	c.response.Body.Close()
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("tool context not cancelled")
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

func TestToolCancellationNotification(t *testing.T) {
	started := make(chan struct{})
	c := start(t, Identity{"mac", "agent", "voice"}, func(ctx context.Context, _ Identity, _ string, _ json.RawMessage) (ToolResult, error) {
		close(started)
		<-ctx.Done()
		return ToolResult{}, ctx.Err()
	})
	c.initialize()
	c.post(`{"jsonrpc":"2.0","id":"cancel-me","method":"tools/call","params":{"name":"create_channel","arguments": {},"_meta":{"progressToken":"p"}}}`, c.identity)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("tool not started")
	}
	c.post(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"cancel-me","reason":"user cancelled"}}`, c.identity)
	_, data := c.read()
	var reply map[string]any
	if err := json.Unmarshal([]byte(data), &reply); err != nil {
		t.Fatal(err)
	}
	errorCode(t, reply, -32800)
}
