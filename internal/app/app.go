package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/aritumn2025/cgb-io-hub/internal/config"
	"github.com/aritumn2025/cgb-io-hub/internal/hub"
	"github.com/aritumn2025/cgb-io-hub/internal/persona"
)

const (
	readHeaderTimeout = 5 * time.Second
	idleTimeout       = 120 * time.Second
)

// App wires together the HTTP server and hub component.
type App struct {
	cfg     config.Config
	logger  *slog.Logger
	hub     *hub.Hub
	persona *persona.Client
	server  *http.Server
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

	var personaClient *persona.Client
	if base := strings.TrimSpace(cfg.DBBaseURL); base != "" {
		client, err := persona.New(persona.Config{
			BaseURL:    base,
			GameName:   cfg.GameID,
			Attraction: cfg.AttractionID,
			Staff:      cfg.StaffName,
			Timeout:    cfg.DBAPITimeout,
		})
		if err != nil {
			return nil, fmt.Errorf("initialise persona client: %w", err)
		}
		personaClient = client
	}

	application := &App{
		cfg:     cfg,
		logger:  logger,
		hub:     hubInstance,
		persona: personaClient,
	}

	mux := application.buildRouter(assets)

	application.server = &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(logger, mux),
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
	}

	return application, nil
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

func (a *App) logErrorWithStack(msg string, args ...any) {
	stack := strings.TrimSpace(string(debug.Stack()))
	fields := append(args, "stack", stack)
	a.logger.Error(msg, fields...)
}
