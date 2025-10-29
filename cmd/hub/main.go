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

	"github.com/59GauthierLab/stg48-backend/internal/app"
	"github.com/59GauthierLab/stg48-backend/internal/config"
)

//go:embed static
var embeddedWeb embed.FS

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "config_error: %v\n", err)
		os.Exit(2)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	staticFS, err := prepareStaticFS()
	if err != nil {
		logger.Error("static_embed_error", "err", err.Error())
		os.Exit(1)
	}

	application, err := app.New(cfg, staticFS, logger)
	if err != nil {
		logger.Error("app_initialise_error", "err", err.Error())
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := application.Run(ctx); err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Error("application_run_error", "err", err.Error())
		}
		os.Exit(1)
	}
}

func prepareStaticFS() (http.FileSystem, error) {
	sub, err := fs.Sub(embeddedWeb, "static")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
