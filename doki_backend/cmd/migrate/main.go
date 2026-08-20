package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

func main() {
	log := logger.New(logger.Config{
		Level:  slog.LevelInfo,
		Output: os.Stdout,
	})
	slog.SetDefault(log)

	log.Info("starting DOKI Schema Migration Runner")

	dbDSN := os.Getenv("DATABASE_URL")
	if dbDSN == "" {
		dbDSN = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dbPool, err := database.NewPool(ctx, dbDSN)
	if err != nil {
		log.Error("failed to connect to database for migrations", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()

	log.Info("database connection verified, ready to apply migrations")
}
