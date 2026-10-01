package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/robfig/cron/v3"
	"github.com/simpul/hr-backend/internal/config"
	"github.com/simpul/hr-backend/internal/jobs"
	"github.com/simpul/hr-backend/internal/platform/database"
)

// How long to wait for Redis at startup before giving up. Bounded so a hanging host fails
// the worker quickly instead of leaving it stuck before it has logged anything useful.
const redisStartupTimeout = 5 * time.Second

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
	// The API is designed to run without Redis; the worker is not — its entire job is to
	// consume a queue that lives there. Without this check the process starts, reports
	// "Starting processing", and then loops forever logging connection errors while doing
	// nothing at all. An orchestrator sees a healthy process, payroll runs stay queued, and
	// the only signal is log noise.
	if cfg.RedisAddr == "" {
		logger.Error("redis is required by the worker", "hint", "set REDIS_ADDR; the API can run without Redis, the worker cannot")
		os.Exit(1)
	}
	redisClient := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	defer redisClient.Close()
	// Fail fast rather than entering the error loop: a worker that cannot reach its queue has
	// nothing to do, and exiting non-zero is what tells the orchestrator to restart it or
	// surface the failure.
	pingCtx, pingCancel := context.WithTimeout(ctx, redisStartupTimeout)
	defer pingCancel()
	if err := redisClient.Ping(pingCtx).Err(); err != nil {
		logger.Error("connect redis", "address", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}
	redisOpt := jobs.AsynqRedis(jobs.RedisConfig{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	client := asynq.NewClient(redisOpt)
	defer client.Close()
	dispatcher := jobs.NewDispatcher(db, client, logger)
	go dispatcher.Run(ctx)
	worker := jobs.NewWorker(db, redisClient, logger)
	server := asynq.NewServer(redisOpt, asynq.Config{Concurrency: 10, Queues: map[string]int{"critical": 6, "default": 3, "low": 1}})
	scheduler := cron.New(cron.WithLocation(time.UTC))
	register := func(spec, topic string) {
		if _, err := scheduler.AddFunc(spec, func() { jobs.ScheduleOrganizationTask(ctx, db, topic, logger) }); err != nil {
			logger.Error("register schedule", "topic", topic, "error", err)
			cancel()
		}
	}
	register("10 * * * *", jobs.TypeAttendanceDailyRollup)
	register("0 18 1 * *", jobs.TypeLeaveAccrual)
	register("0 * * * *", jobs.TypeNotificationDigest)
	register("30 0 * * *", jobs.TypeTHRSchedule)
	scheduler.Start()
	go func() {
		if err := server.Run(worker.Mux()); err != nil {
			logger.Error("worker stopped", "error", err)
			cancel()
		}
	}()
	<-ctx.Done()
	<-scheduler.Stop().Done()
	server.Shutdown()
}
