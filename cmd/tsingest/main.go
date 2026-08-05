package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tsingest/internal/api"
	"tsingest/internal/app"
	"tsingest/internal/worker"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Error("configuration invalid", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	store, err := app.OpenStore(ctx, cfg, log)
	if err != nil {
		log.Error("database initialization failed", "error", err)
		os.Exit(1)
	}
	defer store.DB.Close()
	if cfg.Role == "worker" {
		log.Info("starting recorder worker", "worker_id", cfg.WorkerID, "recordings_root", cfg.RecordingsRoot)
		if err := worker.New(cfg, store, log).Run(ctx); err != nil {
			log.Error("worker stopped with error", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := store.BootstrapAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Error("admin bootstrap failed", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: cfg.ListenAddr, Handler: api.New(cfg, store, log), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 75 * time.Second, WriteTimeout: 0}
	go func() {
		log.Info("starting web server", "address", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("web server failed", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
}
