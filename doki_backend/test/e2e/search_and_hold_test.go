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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	cacheAdapter "doki-backend/internal/adapter/cache/redis"
	httpAdapter "doki-backend/internal/adapter/http"
	"doki-backend/internal/adapter/http/middleware"
	"doki-backend/internal/adapter/http/v1/dto"
	postgresRepo "doki-backend/internal/adapter/repository/postgres"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/domain/property"
	"doki-backend/internal/platform/cache"
	"doki-backend/internal/platform/database"
	"doki-backend/internal/platform/logger"
)

func setupE2EEnvironment(t *testing.T) (*pgxpool.Pool, *redis.Client, http.Handler) {
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
	resRepo := postgresRepo.NewReservationRepository(pool)
	propRepo := postgresRepo.NewPropertyRepository(pool)

	fastHoldAdapter, err := cacheAdapter.NewInventoryHoldAdapter(rdb)
	if err != nil {
		pool.Close()
		rdb.Close()
		t.Fatalf("failed to init fast hold adapter: %v", err)
	}

	holdService := inventory.NewHoldService(fastHoldAdapter, resRepo)

	userRepo := postgresRepo.NewUserRepository(pool)
	propService := property.NewPropertyService(propRepo, userRepo)

	router := httpAdapter.NewRouter(httpAdapter.RouterConfig{
		DB:              pool,
		Redis:           rdb,
		Logger:          log,
		HoldService:     holdService,
		PropertyService: propService,
		Version:         "test-e2e",
	})

	return pool, rdb, router
}

func seedTestProperty(ctx context.Context, t *testing.T, pool *pgxpool.Pool, totalUnits int) (uuid.UUID, uuid.UUID, time.Time, time.Time) {
	propID := uuid.New()
	roomTypeID := uuid.New()
	ownerID := uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO identity.app_user (id, phone_number, full_name, role)
		VALUES ($1, $2, 'E2E Owner', 'HOTEL_OWNER')
		ON CONFLICT (phone_number) DO NOTHING;
	`, ownerID, "+251922"+uuid.New().String()[:6])
	if err != nil {
		t.Fatalf("failed to insert owner: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO property.property (id, code, name, address, city, region, status, owner_id)
		VALUES ($1, $2, 'Skyline Luxury Hotel', 'Bole Atlas', 'Addis Ababa', 'Addis Ababa', 'ACTIVE', $3);
	`, propID, "PROP-"+uuid.New().String()[:8], ownerID)
	if err != nil {
		t.Fatalf("failed to insert property: %v", err)
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO property.room_type (id, property_id, code, name, capacity, base_rate_minor, total_inventory)
		VALUES ($1, $2, $3, 'Executive King Suite', 2, 450000, $4);
	`, roomTypeID, propID, "RT-"+uuid.New().String()[:6], totalUnits)
	if err != nil {
		t.Fatalf("failed to insert room type: %v", err)
	}

	startDate := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	endDate := startDate.Add(4 * 24 * time.Hour)

	for d := startDate; d.Before(endDate); d = d.Add(24 * time.Hour) {
		_, err := pool.Exec(ctx, `
			INSERT INTO inventory.daily_allocations (
				property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
			) VALUES ($1, $2, $3, $4, 0, 0, 450000);
		`, propID, roomTypeID, d.Format("2006-01-02"), totalUnits)
		if err != nil {
			t.Fatalf("failed to insert daily allocation: %v", err)
		}
	}

	return propID, roomTypeID, startDate, endDate
}

func TestSearchProperties_AvailableInventory(t *testing.T) {
	pool, rdb, router := setupE2EEnvironment(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	_, _, startDate, _ := seedTestProperty(ctx, t, pool, 5)

	checkIn := startDate.Format("2006-01-02")
	checkOut := startDate.Add(2 * 24 * time.Hour).Format("2006-01-02")

	url := fmt.Sprintf("/v1/properties/search?check_in=%s&check_out=%s&city=Addis+Ababa&guests=2", checkIn, checkOut)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from search, got %d: %s", rec.Code, rec.Body.String())
	}

	var searchResp dto.SearchPropertiesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}

	if len(searchResp.Results) == 0 {
		t.Fatal("expected at least 1 property in search results")
	}

	firstProp := searchResp.Results[0]
	if len(firstProp.AvailableRoomTypes) == 0 {
		t.Fatal("expected room types to be listed under property")
	}

	rt := firstProp.AvailableRoomTypes[0]
	if rt.UnitsAvailable < 1 {
		t.Errorf("expected units_available > 0, got %d", rt.UnitsAvailable)
	}
	if rt.NightlyRateMinor != 450000 {
		t.Errorf("expected nightly rate 450000, got %d", rt.NightlyRateMinor)
	}
}

func TestCreateHold_Success_Returns201(t *testing.T) {
	pool, rdb, router := setupE2EEnvironment(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	propID, roomTypeID, startDate, _ := seedTestProperty(ctx, t, pool, 3)

	payload := dto.CreateHoldRequest{
		PropertyID: propID.String(),
		RoomTypeID: roomTypeID.String(),
		CheckIn:    startDate.Format("2006-01-02"),
		CheckOut:   startDate.Add(2 * 24 * time.Hour).Format("2006-01-02"),
		GuestName:  "Haile Gebrselassie",
		GuestPhone: "+251911998877",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(middleware.IdempotencyHeader, uuid.New().String())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created from hold endpoint, got %d: %s", rec.Code, rec.Body.String())
	}

	var holdResp dto.CreateHoldResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &holdResp); err != nil {
		t.Fatalf("failed to decode hold response: %v", err)
	}

	if holdResp.ReservationID == "" {
		t.Error("expected valid reservation_id")
	}
	if holdResp.HoldToken == "" {
		t.Error("expected valid hold_token")
	}
	if holdResp.Status != "INVENTORY_HOLD" {
		t.Errorf("expected status 'INVENTORY_HOLD', got '%s'", holdResp.Status)
	}
}

func TestCreateHold_ConcurrentExhaustion_Returns409(t *testing.T) {
	pool, rdb, router := setupE2EEnvironment(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	// Capacity = 2 units
	propID, roomTypeID, startDate, _ := seedTestProperty(ctx, t, pool, 2)

	checkIn := startDate.Format("2006-01-02")
	checkOut := startDate.Add(2 * 24 * time.Hour).Format("2006-01-02")

	// Fire 5 concurrent hold requests for only 2 units
	const concurrentRequests = 5
	var wg sync.WaitGroup
	statusCodes := make(chan int, concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			payload := dto.CreateHoldRequest{
				PropertyID: propID.String(),
				RoomTypeID: roomTypeID.String(),
				CheckIn:    checkIn,
				CheckOut:   checkOut,
				GuestName:  fmt.Sprintf("Concurrent Guest %d", idx),
				GuestPhone: fmt.Sprintf("+25191100000%d", idx),
			}
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(middleware.IdempotencyHeader, uuid.New().String())
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)
			statusCodes <- rec.Code
		}(i)
	}

	wg.Wait()
	close(statusCodes)

	var successCount, conflictCount int
	for code := range statusCodes {
		if code == http.StatusCreated {
			successCount++
		} else if code == http.StatusConflict {
			conflictCount++
		}
	}

	if successCount != 2 {
		t.Errorf("expected exactly 2 successful holds for 2-unit inventory, got %d", successCount)
	}
	if conflictCount != 3 {
		t.Errorf("expected exactly 3 rejected holds (409 Conflict), got %d", conflictCount)
	}
}

func TestCreateHold_IdempotencyKeyReplay(t *testing.T) {
	pool, rdb, router := setupE2EEnvironment(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	propID, roomTypeID, startDate, _ := seedTestProperty(ctx, t, pool, 5)

	idempKey := uuid.New().String()
	payload := dto.CreateHoldRequest{
		PropertyID: propID.String(),
		RoomTypeID: roomTypeID.String(),
		CheckIn:    startDate.Format("2006-01-02"),
		CheckOut:   startDate.Add(2 * 24 * time.Hour).Format("2006-01-02"),
		GuestName:  "Kenenisa Bekele",
		GuestPhone: "+251911334455",
	}
	body, _ := json.Marshal(payload)

	// 1. First execution -> Returns 201 Created
	req1 := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set(middleware.IdempotencyHeader, idempKey)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}

	var resp1 dto.CreateHoldResponse
	_ = json.Unmarshal(rec1.Body.Bytes(), &resp1)

	// 2. Replay with identical Idempotency-Key -> Returns cached 201 with identical payload
	req2 := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set(middleware.IdempotencyHeader, idempKey)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("replayed request expected 201, got %d", rec2.Code)
	}

	if rec2.Header().Get("X-Cache") != "HIT-IDEMPOTENT" {
		t.Errorf("expected X-Cache header HIT-IDEMPOTENT, got %s", rec2.Header().Get("X-Cache"))
	}

	var resp2 dto.CreateHoldResponse
	_ = json.Unmarshal(rec2.Body.Bytes(), &resp2)

	if resp2.ReservationID != resp1.ReservationID {
		t.Errorf("expected replayed reservation ID %s, got %s", resp1.ReservationID, resp2.ReservationID)
	}
	if resp2.HoldToken != resp1.HoldToken {
		t.Errorf("expected replayed hold token %s, got %s", resp1.HoldToken, resp2.HoldToken)
	}
}
