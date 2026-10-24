package linxbridge

import (
	"regexp"
	"testing"
)

func TestConfigReady(t *testing.T) {
	if (Config{URL: DefaultURL, DeviceID: "02:46:50:50:54:01", Token: "token"}).Ready() != true {
		t.Fatal("expected complete config to be ready")
	}
	if (Config{URL: DefaultURL, DeviceID: "", Token: "token"}).Ready() {
		t.Fatal("expected missing device id to disable bridge")
	}
}

func TestDeviceHello(t *testing.T) {
	got := deviceHello()
	if got.Type != "hello" || got.Transport != "websocket" || got.Version != 1 {
		t.Fatalf("unexpected hello: %+v", got)
	}
	if got.AudioParams.Format != "pcm" || got.AudioParams.SampleRate != 16000 || got.AudioParams.FrameSize != 320 {
		t.Fatalf("unexpected audio params: %+v", got.AudioParams)
	}
}

func TestNewClientID(t *testing.T) {
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	first, err := newClientID()
	if err != nil || !pattern.MatchString(first) {
		t.Fatalf("invalid client id %q: %v", first, err)
	}
	second, err := newClientID()
	if err != nil || first == second {
		t.Fatalf("client ids are not unique: %q %q", first, second)
	}
}
