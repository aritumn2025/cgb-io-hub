package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/59GauthierLab/stg48-backend/internal/app"
	"github.com/59GauthierLab/stg48-backend/internal/config"
)

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
