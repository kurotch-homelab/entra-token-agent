package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("tenant_id: tenant\nclient_id: client\nscopes: [scope]\ntoken_file: /tmp/token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, skew, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if skew != 5*time.Minute || c.MetricsAddr != "127.0.0.1:9099" || c.CacheFile != filepath.Join(dir, ".entra-token-agent.cache") {
		t.Fatalf("unexpected defaults: %#v %v", c, skew)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("tenant_id: t\nclient_id: c\nscopes: [s]\ntoken_file: t\ntypo: true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}
