package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/59GauthierLab/cgb-ctrl-hub/internal/config"
	"github.com/59GauthierLab/cgb-ctrl-hub/internal/hub"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
)

// App wires together the HTTP server and hub component.
type App struct {
	cfg    config.Config
	logger *slog.Logger
	hub    *hub.Hub
	server *http.Server
}

// New initialises application state and constructs the HTTP server.
func New(cfg config.Config, assets http.FileSystem, logger *slog.Logger) (*App, error) {
	if logger == nil {
		return nil, errors.New("logger must not be nil")
	}
	if assets == nil {
		return nil, errors.New("assets filesystem must not be nil")
	}

	hubInstance := hub.New(hub.Config{
		AllowedOrigins:  cfg.Origins,
		MaxControllers:  cfg.MaxControllers,
		RelayQueueSize:  cfg.RateHz * 2,
		RegisterTimeout: cfg.RegisterTimeout,
		WriteTimeout:    cfg.WriteTimeout,
	}, logger.With("component", "hub"))

	mux := buildRouter(hubInstance, assets)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	return &App{
		cfg:    cfg,
		logger: logger,
		hub:    hubInstance,
		server: server,
	}, nil
}

// Run starts the HTTP server and blocks until either the context is done or
// the server stops.
func (a *App) Run(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context must not be nil")
	}

	serverErr := make(chan error, 1)
	go func() {
		a.logger.Info("server_listening", "addr", a.cfg.Addr)
		serverErr <- a.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		a.logger.Info("shutdown_signal", "reason", ctx.Err())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()

		a.hub.Shutdown(shutdownCtx)

		if err := a.server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			a.logger.Error("server_shutdown_error", "err", err.Error())
		}

		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}

		a.logger.Info("shutdown_complete")
		return nil

	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
