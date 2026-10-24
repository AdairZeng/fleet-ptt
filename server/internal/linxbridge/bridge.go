// Package linxbridge connects the browser device prototype to the Linx device
// WebSocket protocol. Linx credentials stay on the server.
package linxbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

const (
	DefaultURL      = "wss://xrobo-io.qiniuapi.com/v1/ws/"
	maxControlBytes = 16 << 10
	maxAudioBytes   = 1 << 20
)

type Config struct {
	URL      string
	DeviceID string
	Token    string
}

// Manager keeps the active device credentials in memory and provisions them
// only when an authenticated device asks to power on.
type Manager struct {
	mu        sync.RWMutex
	config    Config
	provision ProvisionConfig
}

func NewManager(config Config, provision ProvisionConfig) *Manager {
	return &Manager{config: config, provision: provision}
}

func (m *Manager) Config() Config {
	if m == nil {
		return Config{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.config
}

func (m *Manager) Ensure(ctx context.Context) (Config, error) {
	if m == nil {
		return Config{}, errors.New("服务器未配置灵矽设备桥接")
	}
	m.mu.RLock()
	current, provision := m.config, m.provision
	m.mu.RUnlock()
	if current.Ready() {
		return current, nil
	}
	provisioned, err := Provision(ctx, provision)
	if err != nil {
		return Config{}, err
	}
	m.mu.Lock()
	m.config = provisioned
	m.mu.Unlock()
	return provisioned, nil
}

func (c Config) Ready() bool {
	return strings.TrimSpace(c.URL) != "" && strings.TrimSpace(c.DeviceID) != "" && strings.TrimSpace(c.Token) != ""
}

type Handler struct {
	Users  *user.Service
	Bridge *Manager
}

type localMessage struct {
	Type  string `json:"type"`
	State string `json:"state,omitempty"`
	Mode  string `json:"mode,omitempty"`
}

type hello struct {
	Type        string      `json:"type"`
	Version     int         `json:"version"`
	Features    any         `json:"features"`
	Transport   string      `json:"transport"`
	AudioParams audioParams `json:"audio_params"`
}

type audioParams struct {
	Format             string `json:"format"`
	SampleRate         int    `json:"sample_rate"`
	Channels           int    `json:"channels"`
	BitDepth           int    `json:"bit_depth,omitempty"`
	Endianness         string `json:"endianness,omitempty"`
	FrameDuration      int    `json:"frame_duration"`
	FrameSize          int    `json:"frame_size,omitempty"`
	PlayBufferDuration int    `json:"play_buffer_duration"`
}

type writeConn struct {
	mu sync.Mutex
	c  *websocket.Conn
}

func (w *writeConn) text(ctx context.Context, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.Write(ctx, websocket.MessageText, b)
}

func (w *writeConn) raw(ctx context.Context, typ websocket.MessageType, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.c.Write(ctx, typ, payload)
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	config := h.Bridge.Config()
	if !config.Ready() {
		http.Error(w, "服务器未配置灵矽设备桥接", http.StatusServiceUnavailable)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if scheme, value, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
			token = strings.TrimSpace(value)
		}
	}
	u, err := h.Users.Authenticate(r.Context(), token)
	if err != nil {
		http.Error(w, "未登录或会话无效", http.StatusUnauthorized)
		return
	}
	if err := h.bindDevice(r.Context(), u, config.DeviceID); err != nil {
		log.Printf("linx bridge: bind device: %v", err)
		http.Error(w, "灵矽设备未绑定当前用户", http.StatusConflict)
		return
	}
	local, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	local.SetReadLimit(maxAudioBytes)
	defer local.CloseNow()

	clientID, err := newClientID()
	if err != nil {
		return
	}
	remote, _, err := websocket.Dial(r.Context(), config.URL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization":    []string{"Bearer " + config.Token},
			"Device-Id":        []string{config.DeviceID},
			"Client-Id":        []string{clientID},
			"Protocol-Version": []string{"1"},
		},
	})
	if err != nil {
		log.Printf("linx bridge: dial: %v", err)
		_ = local.Close(websocket.StatusCode(1011), "灵矽连接失败")
		return
	}
	defer remote.CloseNow()
	remote.SetReadLimit(maxAudioBytes)
	localWriter := &writeConn{c: local}
	remoteWriter := &writeConn{c: remote}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := remoteWriter.text(ctx, deviceHello()); err != nil {
		return
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); h.remoteToLocal(ctx, remote, localWriter, cancel) }()
	go func() { defer wg.Done(); localToRemote(ctx, local, remoteWriter, cancel) }()
	wg.Wait()
}

func (h Handler) bindDevice(ctx context.Context, u store.User, linxDeviceID string) error {
	current, err := h.Users.Store.UserByLinxDeviceID(ctx, linxDeviceID)
	if err == nil {
		if current.DeviceID != u.DeviceID {
			return errors.New("linx device belongs to another application device")
		}
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return h.Users.Store.BindLinxDevice(ctx, u.DeviceID, linxDeviceID)
}

func deviceHello() hello {
	return hello{
		Type: "hello", Version: 1, Features: map[string]any{"mcp": true}, Transport: "websocket",
		AudioParams: audioParams{Format: "pcm", SampleRate: 16000, Channels: 1, BitDepth: 16, Endianness: "little", FrameDuration: 20, FrameSize: 320, PlayBufferDuration: 1000},
	}
}

func (h Handler) remoteToLocal(ctx context.Context, remote *websocket.Conn, local *writeConn, cancel context.CancelFunc) {
	defer cancel()
	for {
		typ, payload, err := remote.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText && typ != websocket.MessageBinary {
			continue
		}
		if typ == websocket.MessageText && !isValidRemoteMessage(payload) {
			return
		}
		if err := local.raw(ctx, typ, payload); err != nil {
			return
		}
	}
}

func isValidRemoteMessage(payload []byte) bool {
	var message struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(payload, &message) != nil || message.Type == "" {
		return false
	}
	return message.Type == "hello" || message.Type == "stt" || message.Type == "tts" || message.Type == "listen" || message.Type == "sentence_start" || message.Type == "llm" || message.Type == "notify" || message.Type == "custom"
}

func localToRemote(ctx context.Context, local *websocket.Conn, remote *writeConn, cancel context.CancelFunc) {
	defer cancel()
	for {
		typ, payload, err := local.Read(ctx)
		if err != nil {
			return
		}
		switch typ {
		case websocket.MessageBinary:
			if len(payload) > maxAudioBytes {
				return
			}
			if err := remote.raw(ctx, typ, payload); err != nil {
				return
			}
		case websocket.MessageText:
			if len(payload) > maxControlBytes {
				return
			}
			var msg localMessage
			if err := json.Unmarshal(payload, &msg); err != nil {
				return
			}
			switch msg.Type {
			case "hello", "ping":
				continue
			case "listen":
				if msg.State != "start" && msg.State != "stop" {
					return
				}
			default:
				return
			}
			if err := remote.raw(ctx, typ, payload); err != nil {
				return
			}
		}
	}
}

func newClientID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("client id: %w", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:])), nil
}
