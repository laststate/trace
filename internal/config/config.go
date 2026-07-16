package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen          string
	PublicURL       string
	DatabaseURL     string
	ObjectDir       string
	MaxEventSize    int64
	MaxArtifactSize int64
	MaxBatchEvents  int
	MaxBatchBytes   int64
	WorkerN         int
	Lease           time.Duration
	Bootstrap       bool
	AdminEmail      string
	AdminPassword   string
	// OpenUI allows unauthenticated GET on UI APIs. Default false (secure).
	OpenUI bool
	// Mode: "all" (API+workers), "api", "worker"
	Mode string

	// HTTP hardening
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
	RateLimitPerMin int
	MaxConnsPerIP   int

	// Retention / GC
	RetentionDays   int
	GCInterval      time.Duration
	EnableRetention bool

	// Queue backend: postgres|memory (nats|redis|cf reserved)
	QueueDriver string
	QueueURL    string

	// S3 / MinIO
	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	// OIDC
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	// Security / enterprise flags
	AllowPublicRegister bool   // TRACE_ALLOW_PUBLIC_REGISTER (default false)
	OIDCAutoJoin        bool   // TRACE_OIDC_AUTO_JOIN first org as developer (default false)
	SAMLInsecure        bool   // TRACE_SAML_INSECURE allow unsigned ACS (default false)
	TrustedProxies      string // TRACE_TRUSTED_PROXIES comma-separated CIDRs/IPs
	SecretsKey          string // TRACE_SECRETS_KEY 32-byte base64/hex for AES-GCM secret at rest
	CookieSecure        bool   // TRACE_COOKIE_SECURE (default true when PublicURL is https)
	AppVersion          string
}

func Load() Config {
	c := Config{
		Listen:          env("TRACE_LISTEN", ":8080"),
		PublicURL:       env("TRACE_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:     env("TRACE_DATABASE_URL", "postgres://trace:trace@localhost:5432/trace?sslmode=disable"),
		ObjectDir:       env("TRACE_OBJECT_DIR", "./data/objects"),
		MaxEventSize:    envInt64("TRACE_MAX_EVENT_SIZE", 4<<20),
		MaxArtifactSize: envInt64("TRACE_MAX_ARTIFACT_SIZE", 64<<20),
		MaxBatchEvents:  int(envInt64("TRACE_MAX_BATCH_EVENTS", 100)),
		MaxBatchBytes:   envInt64("TRACE_MAX_BATCH_BYTES", 50*(4<<20)),
		WorkerN:         int(envInt64("TRACE_WORKERS", 2)),
		Lease:           time.Duration(envInt64("TRACE_JOB_LEASE_SEC", 30)) * time.Second,
		Bootstrap:       env("TRACE_BOOTSTRAP", "true") == "true",
		AdminEmail:      env("TRACE_ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:   env("TRACE_ADMIN_PASSWORD", ""), // empty = generate random; never default to "admin"
		OpenUI:          env("TRACE_OPEN_UI", "false") == "true",
		Mode:            strings.ToLower(env("TRACE_MODE", "all")),

		ReadTimeout:     time.Duration(envInt64("TRACE_HTTP_READ_TIMEOUT_SEC", 30)) * time.Second,
		WriteTimeout:    time.Duration(envInt64("TRACE_HTTP_WRITE_TIMEOUT_SEC", 60)) * time.Second,
		IdleTimeout:     time.Duration(envInt64("TRACE_HTTP_IDLE_TIMEOUT_SEC", 120)) * time.Second,
		MaxHeaderBytes:  int(envInt64("TRACE_HTTP_MAX_HEADER_BYTES", 1<<20)),
		RateLimitPerMin: int(envInt64("TRACE_RATE_LIMIT_PER_MIN", 600)),
		MaxConnsPerIP:   int(envInt64("TRACE_MAX_CONNS_PER_IP", 64)),

		RetentionDays:   int(envInt64("TRACE_RETENTION_DAYS", 90)),
		GCInterval:      time.Duration(envInt64("TRACE_GC_INTERVAL_MIN", 60)) * time.Minute,
		EnableRetention: env("TRACE_ENABLE_RETENTION", "true") == "true",

		QueueDriver: env("TRACE_QUEUE", "postgres"),
		QueueURL:    env("TRACE_QUEUE_URL", ""),

		S3Endpoint:       env("TRACE_S3_ENDPOINT", ""),
		S3Region:         env("TRACE_S3_REGION", "us-east-1"),
		S3Bucket:         env("TRACE_S3_BUCKET", ""),
		S3AccessKey:      env("TRACE_S3_ACCESS_KEY", ""),
		S3SecretKey:      env("TRACE_S3_SECRET_KEY", ""),
		OIDCIssuer:       env("TRACE_OIDC_ISSUER", ""),
		OIDCClientID:     env("TRACE_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("TRACE_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  env("TRACE_OIDC_REDIRECT_URL", ""),

		AllowPublicRegister: env("TRACE_ALLOW_PUBLIC_REGISTER", "false") == "true",
		OIDCAutoJoin:        env("TRACE_OIDC_AUTO_JOIN", "false") == "true",
		SAMLInsecure:        env("TRACE_SAML_INSECURE", "false") == "true",
		TrustedProxies:      env("TRACE_TRUSTED_PROXIES", ""),
		SecretsKey:          env("TRACE_SECRETS_KEY", ""),
		AppVersion:          env("TRACE_VERSION", "0.8.0"),
	}
	if c.OIDCRedirectURL == "" && c.OIDCIssuer != "" {
		c.OIDCRedirectURL = strings.TrimRight(c.PublicURL, "/") + "/api/auth/oidc/callback"
	}
	if c.Mode != "all" && c.Mode != "api" && c.Mode != "worker" {
		c.Mode = "all"
	}
	if env("TRACE_COOKIE_SECURE", "") != "" {
		c.CookieSecure = env("TRACE_COOKIE_SECURE", "false") == "true"
	} else {
		c.CookieSecure = strings.HasPrefix(strings.ToLower(c.PublicURL), "https://")
	}
	return c
}

// ValidateProduction rejects insecure production settings.
func (c Config) ValidateProduction() error {
	if env("TRACE_ENV", "") != "production" && env("TRACE_ENV", "") != "prod" {
		return nil
	}
	if c.OpenUI {
		return fmt.Errorf("TRACE_OPEN_UI must be false in production")
	}
	if c.AdminPassword == "admin" || c.AdminPassword == "password" {
		return fmt.Errorf("insecure TRACE_ADMIN_PASSWORD in production")
	}
	if c.AllowPublicRegister {
		return fmt.Errorf("TRACE_ALLOW_PUBLIC_REGISTER must be false in production")
	}
	if c.SAMLInsecure {
		return fmt.Errorf("TRACE_SAML_INSECURE must be false in production")
	}
	if c.OIDCAutoJoin {
		return fmt.Errorf("TRACE_OIDC_AUTO_JOIN must be false in production")
	}
	return nil
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt64(k string, def int64) int64 {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}
