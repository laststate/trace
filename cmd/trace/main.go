package main

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/laststate/trace/internal/api"
	"github.com/laststate/trace/internal/config"
	"github.com/laststate/trace/internal/db"
	"github.com/laststate/trace/internal/objects"
	"github.com/laststate/trace/internal/queue"
	"github.com/laststate/trace/internal/store"
	"github.com/laststate/trace/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg := config.Load()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}

	obj := &objects.Store{
		Root: cfg.ObjectDir,
		Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey,
	}
	if err := obj.Ensure(); err != nil {
		log.Error("object store", "err", err)
		os.Exit(1)
	}

	st := &store.Store{Pool: pool}
	if cfg.Bootstrap {
		if org, project, secret, err := st.Bootstrap(ctx); err == nil {
			log.Info("bootstrap complete",
				"organization", org.Slug,
				"project", project.Slug,
				"ingest_token", secret,
			)
			tokenPath := filepath.Join(filepath.Dir(cfg.ObjectDir), "bootstrap-token.txt")
			_ = os.MkdirAll(filepath.Dir(tokenPath), 0o755)
			_ = os.WriteFile(tokenPath, []byte(secret+"\n"), 0o600)
		} else if err.Error() != "already bootstrapped" {
			log.Error("bootstrap", "err", err)
			os.Exit(1)
		}
		if u, pw, err := st.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword, "Admin"); err == nil {
			log.Info("admin user created", "email", u.Email, "password", pw)
		} else if err.Error() != "admin already exists" {
			log.Error("admin bootstrap", "err", err)
			os.Exit(1)
		}
	}

	q := &queue.Queue{Pool: pool}
	for i := 0; i < cfg.WorkerN; i++ {
		w := &worker.Worker{Store: st, Queue: q, Object: obj, Lease: cfg.Lease, Log: log}
		go w.Run(ctx)
	}

	webDir := envOr("TRACE_WEB_DIR", "web/dist")
	var ui http.FileSystem
	if stInfo, err := os.Stat(webDir); err == nil && stInfo.IsDir() {
		ui = http.Dir(webDir)
	} else {
		ui = http.FS(emptyFS{})
	}

	srv := &api.Server{Cfg: cfg, Store: st, Object: obj, Log: log, UI: ui}
	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", cfg.Listen, "public_url", cfg.PublicURL, "s3", cfg.S3Endpoint != "", "oidc", cfg.OIDCIssuer != "")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("http", "err", err)
			cancel()
		}
	}()

	<-ctx.Done()
	shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = httpSrv.Shutdown(shutdownCtx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) { return nil, fs.ErrNotExist }
