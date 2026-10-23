package api

import (
	"fmt"
	"net/http"
)

// Healthz 供探活使用，正文为 OK。
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK\n")
}
