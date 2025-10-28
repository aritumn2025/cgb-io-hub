package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/59GauthierLab/stg48-backend/internal/hub"
)

const (
	defaultAddr           = ":8765"
	defaultOrigins        = "*"
	defaultMaxControllers = 4
	defaultRateHz         = 60
	shutdownTimeout       = 10 * time.Second
)

//go:embed static
var embeddedWeb embed.FS

type appConfig struct {
	addr           string
	origins        []string
	maxControllers int
	rateHz         int
}

func main() {
	cfg := loadConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	hubLogger := logger.With("component", "hub")
	hubInstance := hub.New(hub.Config{
		AllowedOrigins:  cfg.origins,
		MaxControllers:  cfg.maxControllers,
		RelayQueueSize:  cfg.rateHz * 2,
		RegisterTimeout: 5 * time.Second,
		WriteTimeout:    2 * time.Second,
	}, hubLogger)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/ws", http.HandlerFunc(hubInstance.HandleWS))

	staticHandler, err := buildStaticHandler()
	if err != nil {
		logger.Error("static_embed_error", "err", err.Error())
		os.Exit(1)
	}
	mux.Handle("/", staticHandler)

	server := &http.Server{
		Addr:              cfg.addr,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server_listening", "addr", cfg.addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown_signal", "reason", ctx.Err())
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_listen_error", "err", err.Error())
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	hubInstance.Shutdown(shutdownCtx)

	if err := server.Shutdown(shutdownCtx); err != nil {
		if !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("server_shutdown_error", "err", err.Error())
		}
	}

	if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server_terminate_error", "err", err.Error())
		os.Exit(1)
	}

	logger.Info("shutdown_complete")
}

func loadConfig() appConfig {
	addrFlag := flag.String("addr", "", "listen address (ADDR)")
	originsFlag := flag.String("origins", "", "allowed origins, comma separated (ORIGINS)")
	maxControllersFlag := flag.Int("max-clients", 0, "max controller connections (MAX_CLIENTS)")
	rateHzFlag := flag.Int("rate-hz", 0, "relay rate limit in Hz (RATE_HZ)")
	flag.Parse()

	addr := firstNonEmpty(*addrFlag, os.Getenv("ADDR"), defaultAddr)
	origins := parseOrigins(firstNonEmpty(*originsFlag, os.Getenv("ORIGINS"), defaultOrigins))
	maxControllers := firstPositive(*maxControllersFlag, envToInt("MAX_CLIENTS"), defaultMaxControllers)
	rateHz := firstPositive(*rateHzFlag, envToInt("RATE_HZ"), defaultRateHz)

	return appConfig{
		addr:           addr,
		origins:        origins,
		maxControllers: maxControllers,
		rateHz:         rateHz,
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func buildStaticHandler() (http.Handler, error) {
	sub, err := fs.Sub(embeddedWeb, "static")
	if err != nil {
		return nil, err
	}
	return http.FileServer(http.FS(sub)), nil
}

func loggingMiddleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &responseLogger{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		duration := time.Since(start)
		logger.Info("http_request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", lrw.status,
			"duration_ms", duration.Milliseconds(),
			"remote_ip", requestIP(r),
		)
	})
}

type responseLogger struct {
	http.ResponseWriter
	status int
}

func (r *responseLogger) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseLogger) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("http.Hijacker not supported")
	}
	return hj.Hijack()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseOrigins(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil
	}
	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func envToInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

func requestIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			candidate := strings.TrimSpace(part)
			if candidate != "" {
				return candidate
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
