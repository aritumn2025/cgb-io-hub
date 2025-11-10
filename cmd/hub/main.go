package main

import (
	"bufio"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/aritumn2025/cgb-io-hub/internal/app"
	"github.com/aritumn2025/cgb-io-hub/internal/config"
)

//go:embed static
var embeddedWeb embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	loadEnvironment()

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

func loadEnvironment() {
	candidates := []string{".env", ".env.example"}
	for _, path := range candidates {
		file, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: failed to read %s: %v\n", path, err)
			continue
		}

		loaded := loadEnvFromReader(file)
		file.Close()

		if loaded {
			return
		}
	}
}

func loadEnvFromReader(r *os.File) bool {
	scanner := bufio.NewScanner(r)
	hasEntry := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		sep := strings.Index(line, "=")
		if sep <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:sep])
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			hasEntry = true
			continue
		}
		value := strings.TrimSpace(line[sep+1:])
		value = strings.Trim(value, `"'`)
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to set %s from env file: %v\n", key, err)
			continue
		}
		hasEntry = true
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to parse env file: %v\n", err)
		return hasEntry
	}
	return hasEntry
}
