package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	cacheAdapter "doki-backend/internal/adapter/cache/redis"
	postgresRepo "doki-backend/internal/adapter/repository/postgres"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
	"doki-backend/internal/platform/worker"
)

var (
	Version   = "1.0.0-dev"
	BuildTime = "unset"
)

func main() {
	var (
		flagRunOnce   = flag.Bool("once", false, "Run single allocation & hold expiry sweep and exit")
		flagDaysAhead = flag.Int("days", 365, "Number of forward days to maintain in inventory horizon")
	)
	flag.Parse()

	log := logger.New(logger.Config{
		Level:  slog.LevelInfo,
		Output: os.Stdout,
	})
	slog.SetDefault(log)

	log.Info("starting DOKI Background Worker",
		slog.String("version", Version),
		slog.String("build_time", BuildTime),
		slog.Bool("run_once", *flagRunOnce),
	)

	dbDSN := os.Getenv("DATABASE_URL")
	if dbDSN == "" {
		dbDSN = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	healthPort := os.Getenv("WORKER_HEALTH_PORT")
	if healthPort == "" {
		healthPort = "9091"
	}

	initCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Initialize Database & Cache pools
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

	log.Info("worker connected to PostgreSQL and Redis successfully")

	// 2. Initialize Repositories, Adapters and Services
	invRepo := postgresRepo.NewInventoryRepository(dbPool)
	propRepo := postgresRepo.NewPropertyRepository(dbPool)
	resRepo := postgresRepo.NewReservationRepository(dbPool)

	fastHoldAdapter, err := cacheAdapter.NewInventoryHoldAdapter(rdb)
	if err != nil {
		log.Error("worker failed to initialize Redis fast hold adapter", slog.String("error", err.Error()))
		os.Exit(1)
	}

	allocService := inventory.NewAllocationService(invRepo, propRepo)
	holdSweeperService := inventory.NewHoldSweeperService(resRepo, fastHoldAdapter, log)

	allocSweeper := worker.NewAllocationSweeper(allocService, log, 24*time.Hour, *flagDaysAhead)
	holdExpiryWorker := worker.NewHoldExpiryWorker(holdSweeperService, log, 15*time.Second, 100)

	// 3. Handle Single-Run CLI Flag (-once)
	if *flagRunOnce {
		log.Info("executing single pass sweep for allocations and hold reconciler (-once mode)")
		if err := allocSweeper.RunOnce(context.Background()); err != nil {
			log.Error("single-pass allocation sweep failed", slog.String("error", err.Error()))
		}
		if err := holdExpiryWorker.RunOnce(context.Background()); err != nil {
			log.Error("single-pass hold expiry sweep failed", slog.String("error", err.Error()))
		}
		log.Info("single-pass worker sweeps completed successfully")
		return
	}

	// 4. Setup Worker Health Probe Server (:9091)
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "UP", "component": "doki-worker"})
	})
	healthMux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := dbPool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "UNAVAILABLE", "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "READY", "component": "doki-worker"})
	})

	healthServer := &http.Server{
		Addr:         ":" + healthPort,
		Handler:      healthMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("starting worker health probe server", slog.String("addr", ":"+healthPort))
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("worker health server error", slog.String("error", err.Error()))
		}
	}()

	// 5. Start Background Sweepers with Signal Context
	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	// Allocation Sweeper (Rolling 365 days)
	go func() {
		if err := allocSweeper.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("allocation sweeper terminated with error", slog.String("error", err.Error()))
		}
	}()

	// Hold Expiry Reconciler (Every 15s)
	go func() {
		if err := holdExpiryWorker.Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("hold expiry worker terminated with error", slog.String("error", err.Error()))
		}
	}()

	// 6. Graceful Teardown on SIGINT / SIGTERM
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	sig := <-shutdown
	log.Info("shutdown signal received, commencing graceful teardown", slog.String("signal", sig.String()))

	workerCancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	_ = healthServer.Shutdown(shutdownCtx)
	log.Info("worker shutdown completed cleanly")
}
