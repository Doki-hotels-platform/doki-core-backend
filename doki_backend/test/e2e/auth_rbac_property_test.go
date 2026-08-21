package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	cacheAdapter "doki-backend/internal/adapter/cache/redis"
	httpAdapter "doki-backend/internal/adapter/http"
	"doki-backend/internal/adapter/http/v1/dto"
	postgresRepo "doki-backend/internal/adapter/repository/postgres"
	"doki-backend/internal/domain/identity"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/domain/property"
	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

var testJWTSecret = []byte("doki-test-jwt-secret-key-12345678901234567890")

func setupAdminE2E(t *testing.T) (*pgxpool.Pool, *redis.Client, http.Handler, *identity.AuthService, *property.PropertyService) {
	dbDSN := os.Getenv("TEST_DATABASE_URL")
	if dbDSN == "" {
		dbDSN = os.Getenv("DATABASE_URL")
	}
	if dbDSN == "" {
		dbDSN = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	redisAddr := os.Getenv("TEST_REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = os.Getenv("REDIS_ADDR")
	}
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, dbDSN)
	if err != nil {
		t.Skipf("skipping E2E test (PostgreSQL unreachable: %v)", err)
	}

	rdb, err := cache.NewRedisClient(ctx, cache.DefaultRedisConfig(redisAddr, "", 0))
	if err != nil {
		pool.Close()
		t.Skipf("skipping E2E test (Redis unreachable: %v)", err)
	}

	log := logger.New(logger.Config{Output: io.Discard})
	userRepo := postgresRepo.NewUserRepository(pool)
	resRepo := postgresRepo.NewReservationRepository(pool)
	propRepo := postgresRepo.NewPropertyRepository(pool)

	authService := identity.NewAuthService(userRepo, testJWTSecret, 24*time.Hour)
	propService := property.NewPropertyService(propRepo, userRepo)

	fastHoldAdapter, err := cacheAdapter.NewInventoryHoldAdapter(rdb)
	if err != nil {
		pool.Close()
		rdb.Close()
		t.Fatalf("failed to init fast hold adapter: %v", err)
	}

	holdService := inventory.NewHoldService(fastHoldAdapter, resRepo)

	router := httpAdapter.NewRouter(httpAdapter.RouterConfig{
		DB:              pool,
		Redis:           rdb,
		Logger:          log,
		HoldService:     holdService,
		PropertyService: propService,
		AuthService:     authService,
		JWTSecret:       testJWTSecret,
		Version:         "test-e2e",
	})

	return pool, rdb, router, authService, propService
}

func TestAuth_RegisterAndLogin_Success(t *testing.T) {
	pool, rdb, router, _, _ := setupAdminE2E(t)
	defer pool.Close()
	defer rdb.Close()

	phone := "+251911" + uuid.New().String()[:6]
	regPayload := dto.RegisterUserRequest{
		PhoneNumber: phone,
		Password:    "SecurePass123!",
		FullName:    "Tirunesh Dibaba",
		Role:        "HOTEL_OWNER",
	}

	body, _ := json.Marshal(regPayload)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("register expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var userResp dto.UserDTO
	_ = json.Unmarshal(rec.Body.Bytes(), &userResp)
	if userResp.PhoneNumber != phone {
		t.Errorf("expected phone %s, got %s", phone, userResp.PhoneNumber)
	}

	// Test Login
	loginPayload := dto.LoginUserRequest{
		Identifier: phone,
		Password:   "SecurePass123!",
	}
	loginBody, _ := json.Marshal(loginPayload)
	loginReq := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()

	router.ServeHTTP(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("login expected 200 OK, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var authResp dto.AuthResponse
	_ = json.Unmarshal(loginRec.Body.Bytes(), &authResp)
	if authResp.Token == "" {
		t.Fatal("expected non-empty JWT token on login")
	}
}

func TestRBAC_HQAdmin_CanAccessAllProperties(t *testing.T) {
	pool, rdb, router, authService, propService := setupAdminE2E(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()

	// 1. Create Property
	prop, err := propService.CreateProperty(ctx, &property.Property{
		Code:     "PROP-HQ-TEST-" + uuid.New().String()[:6],
		Name:     "HQ Test Hotel",
		Category: property.CategoryBranded,
		City:     "Addis Ababa",
		Region:   "Addis Ababa",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create property: %v", err)
	}

	// 2. Register HQ_ADMIN User
	hqPhone := "+251999" + uuid.New().String()[:6]
	_, err = authService.RegisterUser(ctx, identity.RegisterRequest{
		PhoneNumber: hqPhone,
		Password:    "SuperSecretHQ123!",
		FullName:    "Chief Admin",
		Role:        identity.RoleHQAdmin,
	})
	if err != nil {
		t.Fatalf("failed to register HQ Admin: %v", err)
	}

	token, _, err := authService.Login(ctx, hqPhone, "SuperSecretHQ123!")
	if err != nil {
		t.Fatalf("failed to login HQ Admin: %v", err)
	}

	// 3. Request GET /v1/admin/properties/{property_id} as HQ_ADMIN (No direct property assignment needed)
	url := fmt.Sprintf("/v1/admin/properties/%s", prop.ID.String())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HQ_ADMIN expected 200 OK across any property, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRBAC_HotelOwner_ForbiddenCrossProperty(t *testing.T) {
	pool, rdb, router, authService, propService := setupAdminE2E(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()

	// 1. Create Property A owned by Owner A
	ownerAPhone := "+251988" + uuid.New().String()[:6]
	ownerA, _ := authService.RegisterUser(ctx, identity.RegisterRequest{
		PhoneNumber: ownerAPhone,
		Password:    "OwnerAPassword123!",
		FullName:    "Owner Alpha",
		Role:        identity.RoleHotelOwner,
	})

	propA, _ := propService.CreateProperty(ctx, &property.Property{
		Code:     "PROP-A-" + uuid.New().String()[:6],
		Name:     "Hotel Alpha",
		Category: property.CategoryBranded,
		City:     "Addis Ababa",
		Region:   "Addis Ababa",
	}, &ownerA.ID)

	// 2. Create Property B owned by Owner B
	ownerBPhone := "+251977" + uuid.New().String()[:6]
	ownerB, _ := authService.RegisterUser(ctx, identity.RegisterRequest{
		PhoneNumber: ownerBPhone,
		Password:    "OwnerBPassword123!",
		FullName:    "Owner Beta",
		Role:        identity.RoleHotelOwner,
	})

	_, _ = propService.CreateProperty(ctx, &property.Property{
		Code:     "PROP-B-" + uuid.New().String()[:6],
		Name:     "Hotel Beta",
		Category: property.CategoryBranded,
		City:     "Hawassa",
		Region:   "Sidama",
	}, &ownerB.ID)

	// 3. Login Owner B and attempt to access Property A
	tokenB, _, err := authService.Login(ctx, ownerBPhone, "OwnerBPassword123!")
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	url := fmt.Sprintf("/v1/admin/properties/%s", propA.ID.String())
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+tokenB)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	// Owner B must receive 403 Forbidden when attempting to access Property A
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for cross-property access, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProperty_RoomTypeAndRoomCreation(t *testing.T) {
	pool, rdb, router, authService, propService := setupAdminE2E(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()

	// 1. Create Owner & Property
	ownerPhone := "+251966" + uuid.New().String()[:6]
	owner, _ := authService.RegisterUser(ctx, identity.RegisterRequest{
		PhoneNumber: ownerPhone,
		Password:    "OwnerPass123!",
		FullName:    "Meseret Defar",
		Role:        identity.RoleHotelOwner,
	})

	prop, _ := propService.CreateProperty(ctx, &property.Property{
		Code:     "PROP-PROV-" + uuid.New().String()[:6],
		Name:     "Provisioned Hotel",
		Category: property.CategoryBranded,
		City:     "Bishoftu",
		Region:   "Oromia",
	}, &owner.ID)

	token, _, _ := authService.Login(ctx, ownerPhone, "OwnerPass123!")

	// 2. Create Room Type
	rtPayload := dto.CreateRoomTypeRequest{
		Code:           "DELUXE-01",
		Name:           "Deluxe Lake View",
		Capacity:       2,
		BaseRateMinor:  600000,
		TotalInventory: 10,
	}
	body, _ := json.Marshal(rtPayload)
	rtURL := fmt.Sprintf("/v1/admin/properties/%s/room-types", prop.ID.String())
	req := httptest.NewRequest(http.MethodPost, rtURL, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create room type expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var rtResp dto.RoomTypeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &rtResp)
	if rtResp.Code != "DELUXE-01" {
		t.Errorf("expected room type code DELUXE-01, got %s", rtResp.Code)
	}

	// 3. Create Physical Room
	floor := "3"
	rmPayload := dto.CreateRoomRequest{
		RoomTypeID: rtResp.ID,
		RoomNumber: "301",
		Floor:      &floor,
	}
	rmBody, _ := json.Marshal(rmPayload)
	rmURL := fmt.Sprintf("/v1/admin/properties/%s/rooms", prop.ID.String())
	rmReq := httptest.NewRequest(http.MethodPost, rmURL, bytes.NewReader(rmBody))
	rmReq.Header.Set("Authorization", "Bearer "+token)
	rmReq.Header.Set("Content-Type", "application/json")
	rmRec := httptest.NewRecorder()

	router.ServeHTTP(rmRec, rmReq)

	if rmRec.Code != http.StatusCreated {
		t.Fatalf("create physical room expected 201 Created, got %d: %s", rmRec.Code, rmRec.Body.String())
	}
}
