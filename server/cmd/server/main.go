package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxbridge"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
	"github.com/AdairZeng/fleet-ptt/server/internal/store"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

func main() {
	addr := getenv("HTTP_ADDR", ":8080")
	staticDir := getenv("STATIC_DIR", "web/dist")
	dbPath := getenv("SQLITE_PATH", "data/fleet-ptt.db")

	st, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()

	svc := &user.Service{Store: st, Secret: os.Getenv("SESSION_SECRET")}
	if svc.Secret == "" {
		log.Print("SESSION_SECRET is empty; POST /api/v1/session will fail until it is set")
	}
	channels := &channel.Service{Store: st}
	tokens := rtn.Signer{
		AccessKey: os.Getenv("QINIU_ACCESS_KEY"),
		SecretKey: os.Getenv("QINIU_SECRET_KEY"),
		AppID:     os.Getenv("QINIU_RTN_APP_ID"),
	}
	if !tokens.Configured() {
		log.Print("QINIU_ACCESS_KEY, QINIU_SECRET_KEY, or QINIU_RTN_APP_ID is empty; GET /api/v1/channels/{id}/rtn-token will fail until they are set")
	}
	linxToken := os.Getenv("LINX_CHAT_TOKEN")
	if linxToken == "" {
		log.Print("LINX_CHAT_TOKEN is empty; POST /api/v1/linx/chat will reject callbacks until it is set")
	}
	if base := publicBaseURL(os.Getenv("PUBLIC_BASE_URL")); base != "" {
		log.Printf("linx chat callback: %s/api/v1/linx/chat", base)
	}
	bridge := linxbridge.Config{
		URL:      getenv("LINX_WS_URL", linxbridge.DefaultURL),
		DeviceID: os.Getenv("LINX_DEVICE_ID"),
		Token:    os.Getenv("LINX_DEVICE_TOKEN"),
	}
	provision := linxbridge.ProvisionConfig{
		APIKey: os.Getenv("LINX_API_KEY"), AgentID: os.Getenv("LINX_AGENT_ID"), DeviceID: os.Getenv("LINX_DEVICE_ID"),
		APIBaseURL: getenv("LINX_API_BASE_URL", linxbridge.DefaultAPIBaseURL), OTAURL: getenv("LINX_OTA_URL", linxbridge.DefaultOTAURL),
	}
	if provision.Ready() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		provisioned, provisionErr := linxbridge.Provision(ctx, provision)
		cancel()
		if provisionErr != nil {
			log.Printf("linx device provisioning failed: %v", provisionErr)
		} else {
			bridge = provisioned
			log.Printf("linx device provisioned through API: %s", bridge.DeviceID)
		}
	}
	if !bridge.Ready() {
		log.Print("LINX_DEVICE_ID or LINX_DEVICE_TOKEN is empty; /api/v1/linx/ws is disabled")
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(staticDir, svc, channels, tokens, linxToken, bridge),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", addr)
		if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
			log.Printf("serving static files from %s", staticDir)
		}
		errCh <- srv.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// publicBaseURL 返回可写进日志的公网基址。空值、非 http(s)、以及用户信息都不会被打印。
func publicBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/")
}
