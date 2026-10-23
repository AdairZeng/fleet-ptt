package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
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

	srv := &http.Server{
		Addr:              addr,
		Handler:           newHandler(staticDir, svc, channels),
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
