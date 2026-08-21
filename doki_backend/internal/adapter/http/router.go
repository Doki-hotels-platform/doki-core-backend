package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"doki-backend/internal/adapter/http/middleware"
	v1 "doki-backend/internal/adapter/http/v1"
	"doki-backend/internal/domain/identity"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/domain/property"
	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/logger"
)

// RouterConfig contains dependencies required to mount HTTP routes.
type RouterConfig struct {
	DB              *pgxpool.Pool
	Redis           *redis.Client
	Logger          *slog.Logger
	HoldService     *inventory.HoldService
	PropertyService *property.PropertyService
	AuthService     *identity.AuthService
	JWTSecret       []byte
	Version         string
}

// NewRouter constructs the Chi router with middleware stack and domain endpoints.
func NewRouter(cfg RouterConfig) http.Handler {
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(logger.HTTPMiddleware(cfg.Logger))
	r.Use(chimiddleware.Recoverer)

	// Liveness Probe
	r.Get("/livez", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":    "UP",
			"version":   cfg.Version,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		})
	})

	// Readiness Probe
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := make(map[string]string)
		isReady := true

		if err := cfg.DB.Ping(ctx); err != nil {
			checks["postgres"] = fmt.Sprintf("DOWN: %v", err)
			isReady = false
		} else {
			checks["postgres"] = "UP"
		}

		if err := cache.CheckHealth(ctx, cfg.Redis); err != nil {
			checks["redis"] = fmt.Sprintf("DOWN: %v", err)
			isReady = false
		} else {
			checks["redis"] = "UP"
		}

		res := map[string]any{
			"version":   cfg.Version,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
			"checks":    checks,
		}

		if isReady {
			res["status"] = "READY"
			w.WriteHeader(http.StatusOK)
		} else {
			res["status"] = "UNAVAILABLE"
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(res)
	})

	// Handlers
	searchHandler := v1.NewSearchHandler(cfg.DB)
	var propRepo *property.PropertyService
	if cfg.PropertyService != nil {
		propRepo = cfg.PropertyService
	}
	_ = propRepo
	holdHandler := v1.NewHoldHandler(cfg.HoldService, nil)
	authHandler := v1.NewAuthHandler(cfg.AuthService)
	adminPropHandler := v1.NewAdminPropertyHandler(cfg.PropertyService)

	// API v1 Subrouter
	r.Route("/v1", func(v1Router chi.Router) {
		v1Router.Get("/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"service": "doki-hotels-api",
				"version": cfg.Version,
				"status":  "operational",
			})
		})

		// 1. Authentication Endpoints (Public)
		v1Router.Route("/auth", func(authRouter chi.Router) {
			authRouter.Post("/register", authHandler.Register)
			authRouter.Post("/login", authHandler.Login)
		})

		// 2. Public Property Search API
		v1Router.Get("/properties/search", searchHandler.SearchProperties)

		// 3. Short-Lived Inventory Hold API with Idempotency Protection
		v1Router.With(middleware.IdempotencyMiddleware(cfg.Redis)).
			Post("/reservations/hold", holdHandler.CreateHold)

		// 4. Protected Administration & Management APIs (Hierarchical RBAC)
		v1Router.Route("/admin", func(adminRouter chi.Router) {
			adminRouter.Use(middleware.AuthMiddleware(cfg.JWTSecret))

			// Property Provisioning (HQ Admin, Hotel Owner)
			adminRouter.With(middleware.RequireRoles(identity.RoleHQAdmin, identity.RoleHotelOwner)).
				Post("/properties", adminPropHandler.CreateProperty)

			// Property Scoped Endpoints
			adminRouter.Route("/properties/{property_id}", func(propScopeRouter chi.Router) {
				propScopeRouter.Use(middleware.RequirePropertyScope("property_id"))

				// Get property details (All staff assigned to property)
				propScopeRouter.With(middleware.RequireRoles(
					identity.RoleHQAdmin,
					identity.RoleRegionalSupervisor,
					identity.RoleHotelOwner,
					identity.RoleHotelManager,
					identity.RoleReceptionist,
				)).Get("/", adminPropHandler.GetProperty)

				// Update property profile (HQ Admin, Hotel Owner)
				propScopeRouter.With(middleware.RequireRoles(
					identity.RoleHQAdmin,
					identity.RoleHotelOwner,
				)).Put("/", adminPropHandler.UpdateProperty)

				// Create room type (HQ Admin, Hotel Owner, Hotel Manager)
				propScopeRouter.With(middleware.RequireRoles(
					identity.RoleHQAdmin,
					identity.RoleHotelOwner,
					identity.RoleHotelManager,
				)).Post("/room-types", adminPropHandler.CreateRoomType)

				// Create physical room units (HQ Admin, Hotel Owner, Hotel Manager)
				propScopeRouter.With(middleware.RequireRoles(
					identity.RoleHQAdmin,
					identity.RoleHotelOwner,
					identity.RoleHotelManager,
				)).Post("/rooms", adminPropHandler.CreateRoom)
			})
		})
	})

	return r
}
