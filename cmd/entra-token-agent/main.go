package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/kurotch-homelab/entra-token-agent/internal/auth"
	"github.com/kurotch-homelab/entra-token-agent/internal/config"
	"github.com/kurotch-homelab/entra-token-agent/internal/metrics"
	"github.com/kurotch-homelab/entra-token-agent/internal/securefile"
)

var version = "0.0.0-dev"

func main() {
	configPath := flag.String("config", "entra-token-agent.yaml", "path to config file")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("entra-token-agent %s\n", version)
		return
	}
	if err := run(*configPath); err != nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func run(path string) error {
	cfg, skew, err := config.Load(path)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	client, err := public.New(cfg.ClientID, public.WithAuthority("https://login.microsoftonline.com/"+cfg.TenantID), public.WithCache(auth.FileCache{Path: cfg.CacheFile}))
	if err != nil {
		return fmt.Errorf("create MSAL client: %w", err)
	}
	m := &metrics.Metrics{}
	server := &http.Server{Addr: cfg.MetricsAddr, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server", "error", err)
			stop()
		}
	}()
	for {
		result, err := acquire(ctx, client, cfg.Scopes)
		if err != nil {
			m.Failure()
			slog.Error("token acquisition failed", "error", err)
			if !sleep(ctx, 30*time.Second) {
				return nil
			}
			continue
		}
		if err := securefile.Write(cfg.TokenFile, []byte(result.AccessToken)); err != nil {
			m.Failure()
			return fmt.Errorf("write token: %w", err)
		}
		m.Success(result.ExpiresOn)
		slog.Info("token refreshed", "expires", result.ExpiresOn)
		wait := time.Until(result.ExpiresOn.Add(-skew))
		if wait < time.Minute {
			wait = time.Minute
		}
		if !sleep(ctx, wait) {
			return nil
		}
	}
}

func acquire(ctx context.Context, client public.Client, scopes []string) (public.AuthResult, error) {
	accounts, err := client.Accounts(ctx)
	if err == nil {
		for _, account := range accounts {
			if result, silentErr := client.AcquireTokenSilent(ctx, scopes, public.WithSilentAccount(account)); silentErr == nil {
				return result, nil
			}
		}
	}
	code, err := client.AcquireTokenByDeviceCode(ctx, scopes)
	if err != nil {
		return public.AuthResult{}, err
	}
	fmt.Fprintln(os.Stderr, code.Result.Message)
	return code.AuthenticationResult(ctx)
}
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
