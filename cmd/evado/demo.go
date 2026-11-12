package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/nerdlabz/evado-tui/internal/config"
	"github.com/nerdlabz/evado-tui/internal/local"
)

func prepareDemo() (config.Config, func(), error) {
	dir, err := os.MkdirTemp("", "evado-demo-")
	if err != nil {
		return config.Config{}, nil, err
	}
	previous, had := os.LookupEnv("EVADO_CONFIG")
	cleanup := func() {
		_ = os.RemoveAll(dir)
		if had {
			_ = os.Setenv("EVADO_CONFIG", previous)
		} else {
			_ = os.Unsetenv("EVADO_CONFIG")
		}
	}
	if err := os.Setenv("EVADO_CONFIG", filepath.Join(dir, "config.json")); err != nil {
		cleanup()
		return config.Config{}, nil, err
	}
	cfg := config.Config{Local: true, Demo: true}
	if err := cfg.Save(); err != nil {
		cleanup()
		return config.Config{}, nil, err
	}
	b, err := local.Open(filepath.Join(dir, "todos.json"), "", "")
	if err == nil {
		err = b.SeedDemo(context.Background())
	}
	if err != nil {
		cleanup()
		return config.Config{}, nil, err
	}
	return cfg, cleanup, nil
}
