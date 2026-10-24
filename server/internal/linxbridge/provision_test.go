package linxbridge

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvisionBindsActivationCodeThroughAPI(t *testing.T) {
	var otaCalls, bindCalls, activateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/ota":
			otaCalls++
			if r.Header.Get("Device-Id") != "02:46:50:50:54:01" {
				t.Fatalf("device id header %q", r.Header.Get("Device-Id"))
			}
			if otaCalls == 1 {
				_, _ = io.WriteString(w, `{"activation":{"code":"779079"}}`)
			} else {
				_, _ = io.WriteString(w, `{"websocket":{"url":"wss://example.test/ws","token":"device-token"}}`)
			}
		case strings.HasPrefix(r.URL.Path, "/xiaozhi/device/bind/"):
			bindCalls++
			if r.Header.Get("Authorization") != "Bearer api-key" || !strings.HasSuffix(r.URL.Path, "/agent-1/779079") {
				t.Fatalf("bind request %s auth=%q", r.URL.Path, r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, `{"code":0,"message":"设备绑定成功"}`)
		case r.URL.Path == "/activate":
			activateCalls++
			_, _ = io.WriteString(w, `success`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := Provision(context.Background(), ProvisionConfig{
		APIBaseURL: server.URL, OTAURL: server.URL + "/ota", ActivateURL: server.URL + "/activate", APIKey: "api-key", AgentID: "agent-1", MacID: "02:46:50:50:54:01",
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "wss://example.test/ws" || got.Token != "device-token" || got.DeviceID == "" {
		t.Fatalf("unexpected config %+v", got)
	}
	if otaCalls != 2 || bindCalls != 1 || activateCalls != 1 {
		t.Fatalf("calls ota=%d bind=%d activate=%d", otaCalls, bindCalls, activateCalls)
	}
}
