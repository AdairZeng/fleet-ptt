package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/AdairZeng/fleet-ptt/server/internal/api"
	"github.com/AdairZeng/fleet-ptt/server/internal/channel"
	"github.com/AdairZeng/fleet-ptt/server/internal/linxbridge"
	"github.com/AdairZeng/fleet-ptt/server/internal/rtn"
	"github.com/AdairZeng/fleet-ptt/server/internal/user"
)

func newHandler(staticDir string, users *user.Service, channels *channel.Service, tokens rtn.Signer, linxToken, agentID string, managers ...*linxbridge.Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.Healthz)
	api.Register(mux, users, channels, tokens, linxToken, agentID, managers...)
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		mux.Handle("/", staticSite(staticDir))
	}
	return mux
}

func staticSite(root string) http.Handler {
	fs := http.Dir(root)
	files := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
			if f, err := fs.Open(name); err == nil {
				stat, statErr := f.Stat()
				_ = f.Close()
				if statErr == nil && !stat.IsDir() {
					files.ServeHTTP(w, r)
					return
				}
			}
		}
		http.ServeFile(w, r, filepath.Join(root, "index.html"))
	})
}
