package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/kurotch-homelab/entra-token-agent/internal/auth"
	"github.com/kurotch-homelab/entra-token-agent/internal/config"
	"github.com/kurotch-homelab/entra-token-agent/internal/metrics"
	"github.com/kurotch-homelab/entra-token-agent/internal/securefile"
)

type State string

const (
	Pending State = "Starting"
	SignIn  State = "Sign-in required"
	Ready   State = "Ready"
	Failed  State = "Refresh failed"
	Stopped State = "Stopped"
)

type Event struct {
	State   State
	Message string
	Expires time.Time
}

func emit(f func(Event), e Event) {
	if f != nil {
		f(e)
	}
}
func Run(ctx context.Context, path string, notify func(Event)) error {
	cfg, skew, err := config.Load(path)
	if err != nil {
		return err
	}
	client, err := public.New(cfg.ClientID, public.WithAuthority("https://login.microsoftonline.com/"+cfg.TenantID), public.WithCache(auth.FileCache{Path: cfg.CacheFile}))
	if err != nil {
		return fmt.Errorf("create MSAL client: %w", err)
	}
	return run(ctx, cfg, skew, notify, func(ctx context.Context) (public.AuthResult, error) {
		return acquire(ctx, client, cfg.Scopes, notify)
	})
}

func run(ctx context.Context, cfg config.Config, skew time.Duration, notify func(Event), acquireToken func(context.Context) (public.AuthResult, error)) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	m := &metrics.Metrics{}
	listener, err := net.Listen("tcp", cfg.MetricsAddr)
	if err != nil {
		return fmt.Errorf("metrics listen: %w", err)
	}
	server := &http.Server{Addr: cfg.MetricsAddr, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	shutdownDone := make(chan struct{})
	serveDone := make(chan struct{})
	defer func() { stop(); <-shutdownDone; <-serveDone }()
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}()
	go func() {
		defer close(serveDone)
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server", "error", err)
			stop()
		}
	}()
	for {
		result, err := acquireToken(ctx)
		if err != nil {
			m.Failure()
			emit(notify, Event{State: Failed})
			if ctx.Err() != nil {
				return nil
			}
			slog.Error("token acquisition failed", "error", err)
			if !sleep(ctx, 30*time.Second) {
				return nil
			}
			continue
		}
		if err := securefile.Write(cfg.TokenFile, []byte(result.AccessToken)); err != nil {
			m.Failure()
			emit(notify, Event{State: Failed})
			return fmt.Errorf("write token: %w", err)
		}
		m.Success(result.ExpiresOn)
		emit(notify, Event{State: Ready, Expires: result.ExpiresOn})
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

func acquire(ctx context.Context, client public.Client, scopes []string, notify func(Event)) (public.AuthResult, error) {
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
	emit(notify, Event{State: SignIn, Message: code.Result.Message, Expires: code.Result.ExpiresOn})
	defer emit(notify, Event{State: Pending})
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
