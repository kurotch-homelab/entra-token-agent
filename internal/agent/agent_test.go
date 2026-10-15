package agent

import (
	"context"
	"errors"
	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/public"
	"github.com/kurotch-homelab/entra-token-agent/internal/config"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "token")
	var events []Event
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	err = run(ctx, config.Config{TokenFile: path, MetricsAddr: address}, time.Minute, func(e Event) {
		events = append(events, e)
		if e.State == Ready {
			cancel()
		}
	}, func(context.Context) (public.AuthResult, error) {
		return public.AuthResult{AccessToken: "test-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "test-token" {
		t.Fatalf("token write failed: %v", err)
	}
	if len(events) != 1 || events[0].State != Ready {
		t.Fatalf("events: %v", events)
	}
	l, err = net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listener not released on return: %v", err)
	}
	l.Close()
}

func TestFailedAcquisitionCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failed := false
	err := run(ctx, config.Config{MetricsAddr: "127.0.0.1:0"}, 0, func(e Event) {
		if e.State == Failed {
			failed = true
			cancel()
		}
	}, func(context.Context) (public.AuthResult, error) { return public.AuthResult{}, errors.New("offline") })
	if err != nil || !failed {
		t.Fatalf("failed=%v err=%v", failed, err)
	}
}

func TestOccupiedPortDoesNotStartAuthentication(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	called := false
	err = run(context.Background(), config.Config{MetricsAddr: l.Addr().String()}, 0, nil, func(context.Context) (public.AuthResult, error) { called = true; return public.AuthResult{}, nil })
	if err == nil || called {
		t.Fatalf("called=%v err=%v", called, err)
	}
}
