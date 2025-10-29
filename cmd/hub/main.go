package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/59GauthierLab/cgb-backend/internal/app"
	"github.com/59GauthierLab/cgb-backend/internal/config"
)

//go:embed static
var embeddedWeb embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}

		var cfgErr configError
		if errors.As(err, &cfgErr) {
			fmt.Fprintf(os.Stderr, "config_error: %v\n", cfgErr.Unwrap())
			os.Exit(2)
		}

		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

type configError struct {
	err error
}

func (e configError) Error() string {
	return e.err.Error()
}

func (e configError) Unwrap() error {
	return e.err
}

func run(ctx context.Context, args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		return configError{err: err}
	}

	logger := newLogger()

	assets, err := staticAssets()
	if err != nil {
		logger.Error("static_embed_error", "err", err.Error())
		return fmt.Errorf("load static assets: %w", err)
	}

	application, err := app.New(cfg, assets, logger)
	if err != nil {
		logger.Error("app_initialise_error", "err", err.Error())
		return fmt.Errorf("initialise app: %w", err)
	}

	if err := application.Run(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Error("application_run_error", "err", err.Error())
		}
		return err
	}

	return nil
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

func staticAssets() (http.FileSystem, error) {
	sub, err := fs.Sub(embeddedWeb, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
