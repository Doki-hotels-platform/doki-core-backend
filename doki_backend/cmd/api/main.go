package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

var (
	// Version and BuildTime can be injected via ldflags at build time
	Version   = "1.0.0-dev"
	BuildTime = "unset"
)

type Config struct {
	Port         string
	MetricsPort  string
	DatabaseDSN  string
	RedisAddr    string
	RedisPass    string
	RedisDB      int
	LogLevel     slog.Level
	Environment  string
}

func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	metricsPort := os.Getenv("METRICS_PORT")
	if metricsPort == "" {
		metricsPort = "9090"
	}

	dbDSN := os.Getenv("DATABASE_URL")
	if dbDSN == "" {
		dbDSN = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisPass := os.Getenv("REDIS_PASSWORD")

	env := os.Getenv("ENVIRONMENT")
	if env == "" {
		env = "development"
	}

	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}

	return Config{
		Port:        port,
		MetricsPort: metricsPort,
		DatabaseDSN: dbDSN,
		RedisAddr:   redisAddr,
		RedisPass:   redisPass,
		RedisDB:     0,
		LogLevel:    logLevel,
		Environment: env,
	}
}

type HealthResponse struct {
	Status    string            `json:"status"`
	Version   string            `json:"version"`
	Timestamp string            `json:"timestamp"`
	Checks    map[string]string `json:"checks,omitempty"`
}

func main() {
	cfg := loadConfig()

	// Initialize structured JSON logger
	log := logger.New(logger.Config{
		Level:  cfg.LogLevel,
		Output: os.Stdout,
	})
	slog.SetDefault(log)

	log.Info("initializing DOKI Hotels Backend",
		slog.String("version", Version),
		slog.String("build_time", BuildTime),
		slog.String("environment", cfg.Environment),
	)

	// Context for initialization with timeout
	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()

	// Initialize PostgreSQL Connection Pool
	log.Info("connecting to PostgreSQL database...", slog.String("dsn", maskDSN(cfg.DatabaseDSN)))
	dbPool, err := database.NewPool(initCtx, cfg.DatabaseDSN)
	if err != nil {
		log.Error("failed to connect to PostgreSQL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()
	log.Info("PostgreSQL connection pool established and verified (MaxConns: 50, MinConns: 10)")

	// Initialize Redis Client
	log.Info("connecting to Redis cache...", slog.String("addr", cfg.RedisAddr))
	redisClient, err := cache.NewRedisClient(initCtx, cache.DefaultRedisConfig(cfg.RedisAddr, cfg.RedisPass, cfg.RedisDB))
	if err != nil {
		log.Error("failed to connect to Redis", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			log.Warn("error closing Redis client", slog.String("error", err.Error()))
		}
	}()
	log.Info("Redis client connected and ping verified")

	// Setup Main Chi Router
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(logger.HTTPMiddleware(log))
	r.Use(middleware.Recoverer)

	// Liveness and Readiness Probes
	mountHealthEndpoints(r, dbPool, redisClient)

	// API Routes Group
	r.Route("/v1", func(v1 chi.Router) {
		v1.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"service": "doki-hotels-api",
				"version": Version,
				"status":  "operational",
			})
		})
	})

	// Setup Dedicated Metrics Server for Prometheus
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	metricsServer := &http.Server{
		Addr:         ":" + cfg.MetricsPort,
		Handler:      metricsMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("starting Prometheus metrics server", slog.String("addr", ":"+cfg.MetricsPort))
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics server failure", slog.String("error", err.Error()))
		}
	}()

	// Setup API Server
	apiServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Server runner goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("starting DOKI API HTTP server", slog.String("addr", ":"+cfg.Port))
		serverErrors <- apiServer.ListenAndServe()
	}()

	// Graceful Shutdown Listening for SIGINT / SIGTERM
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("fatal HTTP server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	case sig := <-shutdown:
		log.Info("shutdown signal received, commencing graceful teardown", slog.String("signal", sig.String()))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Shutdown HTTP API Server
		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			log.Error("forced shutdown: API server failed to drain connections cleanly", slog.String("error", err.Error()))
			_ = apiServer.Close()
		} else {
			log.Info("API server gracefully stopped")
		}

		// Shutdown Metrics Server
		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Error("forced shutdown: metrics server failed to stop cleanly", slog.String("error", err.Error()))
			_ = metricsServer.Close()
		} else {
			log.Info("Metrics server gracefully stopped")
		}

		log.Info("graceful shutdown sequence completed successfully")
	}
}

// mountHealthEndpoints attaches Kubernetes liveness and readiness probes.
func mountHealthEndpoints(r chi.Router, db *pgxpool.Pool, rdb *redis.Client) {
	// GET /livez — Liveness probe (process is running)
	r.Get("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Status:    "UP",
			Version:   Version,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	})

	// GET /readyz — Readiness probe (validates database, cache, and dependency connectivity)
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := make(map[string]string)
		isReady := true

		// Check PostgreSQL Connection Pool
		if err := db.Ping(ctx); err != nil {
			checks["postgres"] = fmt.Sprintf("DOWN: %v", err)
			isReady = false
		} else {
			checks["postgres"] = "UP"
		}

		// Check Redis Connectivity
		if err := cache.CheckHealth(ctx, rdb); err != nil {
			checks["redis"] = fmt.Sprintf("DOWN: %v", err)
			isReady = false
		} else {
			checks["redis"] = "UP"
		}

		res := HealthResponse{
			Version:   Version,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Checks:    checks,
		}

		if isReady {
			res.Status = "READY"
			w.WriteHeader(http.StatusOK)
		} else {
			res.Status = "UNAVAILABLE"
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		_ = json.NewEncoder(w).Encode(res)
	})
}

func maskDSN(dsn string) string {
	// Simple masking helper for logs
	if len(dsn) > 20 {
		return dsn[:15] + "..." + dsn[len(dsn)-10:]
	}
	return "***"
}
