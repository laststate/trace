package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen       string
	PublicURL    string
	DatabaseURL  string
	ObjectDir    string
	MaxEventSize int64
	WorkerN      int
	Lease        time.Duration
	Bootstrap    bool
}

func Load() Config {
	c := Config{
		Listen:       env("TRACE_LISTEN", ":8080"),
		PublicURL:    env("TRACE_PUBLIC_URL", "http://localhost:8080"),
		DatabaseURL:  env("TRACE_DATABASE_URL", "postgres://trace:trace@localhost:5432/trace?sslmode=disable"),
		ObjectDir:    env("TRACE_OBJECT_DIR", "./data/objects"),
		MaxEventSize: envInt64("TRACE_MAX_EVENT_SIZE", 4<<20),
		WorkerN:      int(envInt64("TRACE_WORKERS", 2)),
		Lease:        time.Duration(envInt64("TRACE_JOB_LEASE_SEC", 30)) * time.Second,
		Bootstrap:    env("TRACE_BOOTSTRAP", "true") == "true",
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
