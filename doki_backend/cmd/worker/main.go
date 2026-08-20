package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

var (
	Version   = "1.0.0-dev"
	BuildTime = "unset"
)

func main() {
	log := logger.New(logger.Config{
		Level:  slog.LevelInfo,
		Output: os.Stdout,
	})
	slog.SetDefault(log)

	log.Info("starting DOKI Background Worker",
		slog.String("version", Version),
		slog.String("build_time", BuildTime),
	)

	dbDSN := os.Getenv("DATABASE_URL")
	if dbDSN == "" {
		dbDSN = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPool, err := database.NewPool(initCtx, dbDSN)
	if err != nil {
		log.Error("worker failed to connect to PostgreSQL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	rdb, err := cache.NewRedisClient(initCtx, cache.DefaultRedisConfig(redisAddr, "", 0))
	if err != nil {
		log.Error("worker failed to connect to Redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = rdb.Close() }()

	log.Info("worker subsystems initialized (outbox relay & hold expiry sweeper scaffold)")

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	<-shutdown
	log.Info("worker shutting down cleanly")
}
