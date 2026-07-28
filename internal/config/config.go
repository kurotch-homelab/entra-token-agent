package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TenantID    string   `yaml:"tenant_id"`
	ClientID    string   `yaml:"client_id"`
	Scopes      []string `yaml:"scopes"`
	TokenFile   string   `yaml:"token_file"`
	CacheFile   string   `yaml:"cache_file"`
	RefreshSkew string   `yaml:"refresh_skew"`
	MetricsAddr string   `yaml:"metrics_addr"`
}

func Load(path string) (Config, time.Duration, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, 0, fmt.Errorf("read config: %w", err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, 0, fmt.Errorf("parse config: %w", err)
	}
	if c.TenantID == "" || c.ClientID == "" || len(c.Scopes) == 0 || c.TokenFile == "" {
		return c, 0, fmt.Errorf("tenant_id, client_id, scopes, and token_file are required")
	}
	if c.RefreshSkew == "" {
		c.RefreshSkew = "5m"
	}
	skew, err := time.ParseDuration(c.RefreshSkew)
	if err != nil || skew < 0 {
		return c, 0, fmt.Errorf("invalid refresh_skew %q", c.RefreshSkew)
	}
	if c.MetricsAddr == "" {
		c.MetricsAddr = "127.0.0.1:9099"
	}
	if c.CacheFile == "" {
		c.CacheFile = filepath.Join(filepath.Dir(path), ".entra-token-agent.cache")
	}
	return c, skew, nil
}
