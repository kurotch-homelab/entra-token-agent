//go:build windows

package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/kurotch-homelab/entra-token-agent/internal/agent"
	"github.com/kurotch-homelab/entra-token-agent/internal/tray"
)

var version = "0.0.0-dev"

func main() {
	path := flag.String("config", "entra-token-agent.yaml", "path to config file")
	flag.Parse()
	absolute, err := filepath.Abs(*path)
	if err != nil {
		tray.Alert(err)
		return
	}
	dir := filepath.Dir(absolute)
	log, err := os.OpenFile(filepath.Join(dir, "agent-stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		tray.Alert(err)
		return
	}
	defer log.Close()
	stdout, err := os.OpenFile(filepath.Join(dir, "agent-stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		tray.Alert(err)
		return
	}
	defer stdout.Close()
	os.Stderr = log
	os.Stdout = stdout
	slog.SetDefault(slog.New(slog.NewTextHandler(log, nil)))
	slog.Info("tray starting", "version", version)
	if err := tray.Run(dir, func(ctx context.Context, notify func(agent.Event)) error {
		return agent.Run(ctx, absolute, notify)
	}); err != nil {
		slog.Error("tray stopped", "error", err)
		tray.Alert(err)
	}
}
