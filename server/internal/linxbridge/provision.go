package linxbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultAPIBaseURL = "https://xrobo.qiniu.com"
	DefaultOTAURL     = "https://xrobo.qiniuapi.com/v1/ota/"
)

// ProvisionConfig contains the information needed to activate a virtual
// device through the official Linx API. APIKey and AgentID stay server-side.
type ProvisionConfig struct {
	APIBaseURL  string
	OTAURL      string
	ActivateURL string
	APIKey      string
	AgentID     string
	DeviceID    string
	ClientID    string
	HTTPClient  *http.Client
}

func (c ProvisionConfig) Ready() bool {
	return strings.TrimSpace(c.APIKey) != "" && strings.TrimSpace(c.AgentID) != "" && strings.TrimSpace(c.DeviceID) != ""
}

type provisionResponse struct {
	Activation *struct {
		Code string `json:"code"`
	} `json:"activation,omitempty"`
	WebSocket *struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	} `json:"websocket,omitempty"`
	Error string `json:"error,omitempty"`
}

type apiResponse struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Message string `json:"message"`
}

// Provision activates and binds the virtual device without a console step.
// It returns the WebSocket settings that can be used by Config.
func Provision(ctx context.Context, cfg ProvisionConfig) (Config, error) {
	if !cfg.Ready() {
		return Config{}, errors.New("灵矽自动绑定配置不完整")
	}
	if cfg.APIBaseURL == "" {
		cfg.APIBaseURL = DefaultAPIBaseURL
	}
	if cfg.OTAURL == "" {
		cfg.OTAURL = DefaultOTAURL
	}
	if cfg.ActivateURL == "" {
		cfg.ActivateURL = "https://xrobo.qiniuapi.com/v1/ota/activate"
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.ClientID == "" {
		var err error
		cfg.ClientID, err = newClientID()
		if err != nil {
			return Config{}, err
		}
	}
	first, err := cfg.ota(ctx)
	if err != nil {
		return Config{}, err
	}
	if first.WebSocket == nil || first.WebSocket.Token == "" {
		if first.Activation == nil || first.Activation.Code == "" {
			return Config{}, errors.New("灵矽未返回设备激活码")
		}
		if err := cfg.bind(ctx, first.Activation.Code); err != nil {
			return Config{}, err
		}
		if err := cfg.activate(ctx); err != nil {
			return Config{}, err
		}
		first, err = cfg.ota(ctx)
		if err != nil {
			return Config{}, err
		}
	}
	if first.WebSocket == nil || first.WebSocket.Token == "" {
		return Config{}, errors.New("灵矽设备绑定后未返回 WebSocket 凭证")
	}
	url := first.WebSocket.URL
	if url == "" {
		url = DefaultURL
	}
	return Config{URL: url, DeviceID: cfg.DeviceID, Token: first.WebSocket.Token}, nil
}

func (c ProvisionConfig) ota(ctx context.Context) (provisionResponse, error) {
	body := map[string]any{
		"version": 0, "uuid": c.ClientID,
		"application": map[string]any{"name": "fleet-ptt-3d-prototype", "version": "0.1.0", "compile_time": time.Now().UTC().Format("2006-01-02 15:04:05"), "elf_sha256": ""},
		"ota":         map[string]string{"label": "fleet-ptt-3d-prototype"},
		"board":       map[string]any{"type": "fleet-ptt-web", "ssid": "browser", "rssi": 0, "channel": 0, "ip": "127.0.0.1", "mac": c.DeviceID},
		"mac_address": c.DeviceID, "flash_size": 0, "minimum_free_heap_size": 0, "chip_model_name": "", "partition_table": []any{},
	}
	return c.doJSON(ctx, http.MethodPost, c.OTAURL, map[string]string{"Activation-Version": "1", "Device-Id": c.DeviceID, "Client-Id": c.ClientID, "User-Agent": "fleet-ptt-3d-prototype/0.1.0", "Accept-Language": "zh-CN"}, body)
}

func (c ProvisionConfig) bind(ctx context.Context, code string) error {
	var response apiResponse
	if err := c.doAPI(ctx, http.MethodPost, fmt.Sprintf("%s/xiaozhi/device/bind/%s/%s", strings.TrimRight(c.APIBaseURL, "/"), c.AgentID, code), &response); err != nil {
		return err
	}
	if response.Code != 0 {
		message := response.Msg
		if message == "" {
			message = response.Message
		}
		return fmt.Errorf("灵矽设备绑定失败：%s", message)
	}
	return nil
}

func (c ProvisionConfig) activate(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ActivateURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Device-Id", c.DeviceID)
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("灵矽设备激活失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("灵矽设备激活失败：HTTP %d", response.StatusCode)
	}
	return nil
}

func (c ProvisionConfig) doAPI(ctx context.Context, method, url string, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("灵矽 API 请求失败：%w", err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("灵矽 API 响应无效：%w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("灵矽 API 请求失败：HTTP %d", response.StatusCode)
	}
	return nil
}

func (c ProvisionConfig) doJSON(ctx context.Context, method, url string, headers map[string]string, body any) (provisionResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return provisionResponse{}, err
	}
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return provisionResponse{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return provisionResponse{}, fmt.Errorf("灵矽 OTA 请求失败：%w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return provisionResponse{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return provisionResponse{}, fmt.Errorf("灵矽 OTA 请求失败：HTTP %d", response.StatusCode)
	}
	var result provisionResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return provisionResponse{}, fmt.Errorf("灵矽 OTA 响应无效：%w", err)
	}
	if result.Error != "" {
		return provisionResponse{}, errors.New("灵矽 OTA：" + result.Error)
	}
	return result, nil
}
