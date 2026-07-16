package config

import (
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
	WorkerN         int
	Lease           time.Duration
	Bootstrap       bool
	AdminEmail      string
	AdminPassword   string
	OpenUI          bool

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
}

func Load() Config {
	c := Config{
		Listen:          env("TRACE_LISTEN", ":8080"),
		PublicURL:       env("TRACE_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:     env("TRACE_DATABASE_URL", "postgres://trace:trace@localhost:5432/trace?sslmode=disable"),
		ObjectDir:       env("TRACE_OBJECT_DIR", "./data/objects"),
		MaxEventSize:    envInt64("TRACE_MAX_EVENT_SIZE", 4<<20),
		MaxArtifactSize: envInt64("TRACE_MAX_ARTIFACT_SIZE", 64<<20),
		WorkerN:         int(envInt64("TRACE_WORKERS", 2)),
		Lease:           time.Duration(envInt64("TRACE_JOB_LEASE_SEC", 30)) * time.Second,
		Bootstrap:       env("TRACE_BOOTSTRAP", "true") == "true",
		AdminEmail:      env("TRACE_ADMIN_EMAIL", "admin@localhost"),
		AdminPassword:   env("TRACE_ADMIN_PASSWORD", "admin"),
		OpenUI:          env("TRACE_OPEN_UI", "true") == "true",
		S3Endpoint:      env("TRACE_S3_ENDPOINT", ""),
		S3Region:        env("TRACE_S3_REGION", "us-east-1"),
		S3Bucket:        env("TRACE_S3_BUCKET", ""),
		S3AccessKey:     env("TRACE_S3_ACCESS_KEY", ""),
		S3SecretKey:     env("TRACE_S3_SECRET_KEY", ""),
		OIDCIssuer:      env("TRACE_OIDC_ISSUER", ""),
		OIDCClientID:    env("TRACE_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("TRACE_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL: env("TRACE_OIDC_REDIRECT_URL", ""),
	}
	if c.OIDCRedirectURL == "" && c.OIDCIssuer != "" {
		c.OIDCRedirectURL = strings.TrimRight(c.PublicURL, "/") + "/api/auth/oidc/callback"
	}
	return c
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
