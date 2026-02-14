package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"real-llm/internal/server"
)

func main() {
	configPath := flag.String("config", "", "Path to server config YAML/JSON")
	listen := flag.String("listen", "", "Optional listen address override")
	seedUser := flag.Bool("seed-user", false, "Create/update user and exit")
	username := flag.String("username", "", "Username for seed-user")
	password := flag.String("password", "", "Password for seed-user")
	role := flag.String("role", "admin", "Role for seed-user (admin|user)")
	flag.Parse()

	cfg, err := server.LoadServerConfig(*configPath)
	if err != nil {
		slog.Error("load config failed", "error", err)
		os.Exit(1)
	}
	if *listen != "" {
		cfg.ListenAddr = *listen
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Connect to PostgreSQL
	poolCfg, err := pgxpool.ParseConfig(cfg.Database.DSN)
	if err != nil {
		slog.Error("parse database DSN failed", "error", err)
		os.Exit(1)
	}
	if cfg.Database.MaxConns > 0 {
		poolCfg.MaxConns = cfg.Database.MaxConns
	}
	pool, err := pgxpool.NewWithConfig(rootCtx, poolCfg)
	if err != nil {
		slog.Error("connect database failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Run migrations
	if err := server.RunMigrations(rootCtx, pool, cfg.Database.MigrationsPath); err != nil {
		slog.Error("run migrations failed", "error", err)
		os.Exit(1)
	}

	// Seed user mode
	if *seedUser {
		if *username == "" || *password == "" {
			fmt.Fprintln(os.Stderr, "seed-user requires -username and -password")
			os.Exit(1)
		}
		if err := server.SeedUser(rootCtx, pool, *username, *password, *role); err != nil {
			slog.Error("seed user failed", "error", err)
			os.Exit(1)
		}
		slog.Info("user seeded", "username", *username, "role", *role)
		return
	}

	obs, err := server.SetupObservability(rootCtx, cfg.Observer)
	if err != nil {
		slog.Error("setup observability failed", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = obs.Shutdown(ctx)
	}()

	store := server.NewPgStore(pool)
	auth := server.NewAuth(pool, cfg)
	budget := server.NewBudgetManager(cfg)
	runner := server.NewRunManager(cfg, store, budget, obs)
	defer runner.Shutdown()

	api := server.NewAPI(auth, store, runner, obs)
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-rootCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	slog.Info("probe API listening",
		"listen", cfg.ListenAddr,
		"database", cfg.Database.DSN,
	)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
