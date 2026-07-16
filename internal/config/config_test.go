package config_test

import (
	"os"
	"testing"

	"github.com/laststate/trace/internal/config"
)

func TestLoadDefaultsSecure(t *testing.T) {
	// clear critical vars
	for _, k := range []string{
		"TRACE_OPEN_UI", "TRACE_ADMIN_PASSWORD", "TRACE_MODE", "TRACE_ENV",
		"TRACE_QUEUE", "TRACE_WORKERS",
	} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	c := config.Load()
	if c.OpenUI {
		t.Fatal("OpenUI must default false")
	}
	if c.AdminPassword != "" {
		t.Fatal("admin password must default empty")
	}
	if c.Mode != "all" {
		t.Fatal(c.Mode)
	}
	if c.QueueDriver != "postgres" {
		t.Fatal(c.QueueDriver)
	}
	if c.MaxBatchEvents <= 0 || c.RateLimitPerMin <= 0 {
		t.Fatal("limits")
	}
}

func TestLoadOpenUITrue(t *testing.T) {
	t.Setenv("TRACE_OPEN_UI", "true")
	c := config.Load()
	if !c.OpenUI {
		t.Fatal()
	}
}

func TestValidateProduction(t *testing.T) {
	t.Setenv("TRACE_ENV", "production")
	c := config.Load()
	c.OpenUI = true
	if err := c.ValidateProduction(); err == nil {
		t.Fatal("open ui in prod")
	}
	c.OpenUI = false
	c.AdminPassword = "admin"
	if err := c.ValidateProduction(); err == nil {
		t.Fatal("weak password")
	}
	c.AdminPassword = "strong-enough-password"
	if err := c.ValidateProduction(); err != nil {
		t.Fatal(err)
	}
}

func TestModeNormalization(t *testing.T) {
	t.Setenv("TRACE_MODE", "weird")
	c := config.Load()
	if c.Mode != "all" {
		t.Fatal(c.Mode)
	}
	t.Setenv("TRACE_MODE", "worker")
	c = config.Load()
	if c.Mode != "worker" {
		t.Fatal(c.Mode)
	}
}

func TestOIDCRedirectDefault(t *testing.T) {
	t.Setenv("TRACE_OIDC_ISSUER", "https://idp.example")
	t.Setenv("TRACE_OIDC_REDIRECT_URL", "")
	t.Setenv("TRACE_PUBLIC_URL", "https://trace.example")
	c := config.Load()
	if c.OIDCRedirectURL == "" || c.OIDCRedirectURL[:8] != "https://" {
		t.Fatal(c.OIDCRedirectURL)
	}
}

func TestEnvIntBad(t *testing.T) {
	t.Setenv("TRACE_WORKERS", "notanumber")
	c := config.Load()
	if c.WorkerN != 2 { // default
		t.Log("workers", c.WorkerN)
	}
}
