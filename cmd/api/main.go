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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	cacheAdapter "doki-backend/internal/adapter/cache/redis"
	httpAdapter "doki-backend/internal/adapter/http"
	postgresRepo "doki-backend/internal/adapter/repository/postgres"
	"doki-backend/internal/domain/identity"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/domain/property"
	"doki-backend/internal/platform/auth"
	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

var (
	Version   = "1.0.0-dev"
	BuildTime = "unset"
)

type Config struct {
	Port        string
	MetricsPort string
	DatabaseDSN string
	RedisAddr   string
	RedisPass   string
	RedisDB     int
	JWTSecret   string
	LogLevel    slog.Level
	Environment string
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "doki-dev-jwt-secret-key-change-in-production-min-32-chars"
	}

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
		JWTSecret:   jwtSecret,
		LogLevel:    logLevel,
		Environment: env,
	}
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

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()

	// 1. Initialize PostgreSQL Connection Pool
	log.Info("connecting to PostgreSQL database...", slog.String("dsn", maskDSN(cfg.DatabaseDSN)))
	dbPool, err := database.NewPool(initCtx, cfg.DatabaseDSN)
	if err != nil {
		log.Error("failed to connect to PostgreSQL", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer dbPool.Close()
	log.Info("PostgreSQL connection pool established (MaxConns: 50, MinConns: 10)")

	// 2. Initialize Redis Client
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

	// 3. Initialize Domain Repositories and Services
	userRepo := postgresRepo.NewUserRepository(dbPool)
	resRepo := postgresRepo.NewReservationRepository(dbPool)
	propRepo := postgresRepo.NewPropertyRepository(dbPool)

	tokenIssuer := auth.NewJWTTokenIssuer([]byte(cfg.JWTSecret), 24*time.Hour)
	authService := identity.NewAuthService(userRepo, tokenIssuer)
	propService := property.NewPropertyService(propRepo, userRepo)

	fastHoldAdapter, err := cacheAdapter.NewInventoryHoldAdapter(redisClient)
	if err != nil {
		log.Error("failed to initialize Redis inventory hold adapter", slog.String("error", err.Error()))
		os.Exit(1)
	}

	holdService := inventory.NewHoldService(fastHoldAdapter, resRepo)

	// 4. Construct HTTP Router with Auth, Search, Hold, and Admin RBAC routes
	router := httpAdapter.NewRouter(httpAdapter.RouterConfig{
		DB:              dbPool,
		Redis:           redisClient,
		Logger:          log,
		HoldService:     holdService,
		PropertyService: propService,
		AuthService:     authService,
		JWTSecret:       []byte(cfg.JWTSecret),
		Version:         Version,
	})

	// 5. Dedicated Metrics Server (:9090)
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

	// 6. Main HTTP API Server (:8080)
	apiServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Info("starting DOKI API HTTP server", slog.String("addr", ":"+cfg.Port))
		serverErrors <- apiServer.ListenAndServe()
	}()

	// 7. Graceful Shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("fatal HTTP server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	case sig := <-shutdown:
		log.Info("shutdown signal received, draining active connections", slog.String("signal", sig.String()))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := apiServer.Shutdown(shutdownCtx); err != nil {
			log.Error("forced shutdown: API server failed to drain cleanly", slog.String("error", err.Error()))
			_ = apiServer.Close()
		}

		if err := metricsServer.Shutdown(shutdownCtx); err != nil {
			log.Error("forced shutdown: metrics server failed to stop cleanly", slog.String("error", err.Error()))
			_ = metricsServer.Close()
		}

		log.Info("graceful shutdown sequence completed successfully")
	}
}

func maskDSN(dsn string) string {
	if len(dsn) > 24 {
		return dsn[:12] + "..." + dsn[len(dsn)-10:]
	}
	return "***"
}
