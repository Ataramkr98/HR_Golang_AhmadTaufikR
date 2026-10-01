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

	"github.com/redis/go-redis/v9"
	"github.com/simpul/hr-backend/internal/config"
	"github.com/simpul/hr-backend/internal/httpapi"
	"github.com/simpul/hr-backend/internal/platform/database"
	"github.com/simpul/hr-backend/internal/schemacheck"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	db, err := database.Open(ctx, cfg.DatabaseURL, cfg.Environment == "development")
	if err != nil {
		logger.Error("connect database", "error", err)
		os.Exit(1)
	}
	// Refuse to start against a database that is not ours. Every route would otherwise answer
	// 500 from a column that does not exist, and the readiness probe would still report the
	// database as "ok" — a service that is up, reachable, and unable to serve a single request.
	// Failing here instead makes the cause a single startup log line.
	if err := schemacheck.Verify(db); err != nil {
		logger.Error("schema check failed", "error", err)
		os.Exit(1)
	}
	// Redis is optional. An empty REDIS_ADDR means this deployment does not use it, so the
	// client must stay nil rather than being constructed with an empty address — go-redis
	// would silently fall back to localhost:6379 and every consumer would behave as though a
	// Redis were configured but broken.
	//
	// The API does not enqueue jobs: it writes outbox rows and the worker dispatches them,
	// which is why no queue client is built here.
	var redisClient *redis.Client
	if cfg.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
		defer redisClient.Close()
	}
	server, err := httpapi.NewServer(cfg, db, redisClient, logger)
	if err != nil {
		logger.Error("create server", "error", err)
		os.Exit(1)
	}
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: server.Router(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api failed", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("api shutdown", "error", err)
	}
}
