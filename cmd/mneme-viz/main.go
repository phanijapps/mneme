// Package main serves the mneme-viz dashboard: embedded HTML at / plus a
// reverse proxy for /api/v1/* so the frontend never hits CORS.
package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phanijapps/mneme/internal/viz"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	apiURL, err := url.Parse(envOr("MNEME_API_URL", "http://localhost:8080"))
	if err != nil {
		logger.Error("invalid MNEME_API_URL", "err", err)
		os.Exit(1)
	}
	addr := envOr("VIZ_PORT", ":8090")

	indexHTML, err := viz.Index()
	if err != nil {
		logger.Error("embed read", "err", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// Proxy /api/v1/* to the mneme API. Auth headers pass through untouched.
	proxy := httputil.NewSingleHostReverseProxy(apiURL)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Warn("proxy upstream error", "upstream", apiURL.String(), "path", r.URL.Path, "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"code":"BAD_GATEWAY","message":"mneme API unreachable at ` + apiURL.String() + `"}}`))
	}
	mux.Handle("/api/v1/", proxy)

	// Serve the embedded dashboard at / (and any other non-API path).
	static, err := fs.Sub(viz.FS, ".")
	if err != nil {
		logger.Error("fs sub", "err", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.FS(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Single-page app: unknown paths get the dashboard, / gets index.html.
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(indexHTML)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		logger.Info("mneme-viz listening", "addr", addr, "api", apiURL.String())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("serve", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
	logger.Info("stopped")
}
