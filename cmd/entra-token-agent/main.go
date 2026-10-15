package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/kurotch-homelab/entra-token-agent/internal/agent"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

var version = "0.0.0-dev"

func main() {
	path := flag.String("config", "entra-token-agent.yaml", "path to config file")
	ver := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *ver {
		fmt.Printf("entra-token-agent %s\n", version)
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Run(ctx, *path, func(e agent.Event) {
		if e.State == agent.SignIn {
			fmt.Fprintln(os.Stderr, e.Message)
		}
	}); err != nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
