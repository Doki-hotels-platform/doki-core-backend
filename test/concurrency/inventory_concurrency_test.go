package concurrency

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
	"sync/atomic"
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

var testJWTSecret = []byte("doki-concurrency-test-secret-key-32chars")

func setupConcurrencyStack(t *testing.T) (*pgxpool.Pool, *redis.Client, http.Handler, *inventory.HoldService, *postgresRepo.InventoryRepository) {
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
		t.Skipf("skipping concurrency test (PostgreSQL unreachable: %v)", err)
	}

	rdb, err := cache.NewRedisClient(ctx, cache.DefaultRedisConfig(redisAddr, "", 0))
	if err != nil {
		pool.Close()
		t.Skipf("skipping concurrency test (Redis unreachable: %v)", err)
	}

	log := logger.New(logger.Config{Output: io.Discard})
	userRepo := postgresRepo.NewUserRepository(pool)
	resRepo := postgresRepo.NewReservationRepository(pool)
	propRepo := postgresRepo.NewPropertyRepository(pool)
	invRepo := postgresRepo.NewInventoryRepository(pool)

	fastHoldAdapter, err := cacheAdapter.NewInventoryHoldAdapter(rdb)
	if err != nil {
		pool.Close()
		rdb.Close()
		t.Fatalf("failed to init fast hold adapter: %v", err)
	}

	authService := identity.NewAuthService(userRepo, testJWTSecret, 24*time.Hour)
	propService := property.NewPropertyService(propRepo, userRepo)
	holdService := inventory.NewHoldService(fastHoldAdapter, resRepo)

	router := httpAdapter.NewRouter(httpAdapter.RouterConfig{
		DB:              pool,
		Redis:           rdb,
		Logger:          log,
		HoldService:     holdService,
		PropertyService: propService,
		AuthService:     authService,
		JWTSecret:       testJWTSecret,
		Version:         "test-concurrency",
	})

	return pool, rdb, router, holdService, invRepo
}

func seedConcurrencyProperty(ctx context.Context, t *testing.T, pool *pgxpool.Pool, totalUnits int) (uuid.UUID, uuid.UUID, time.Time, time.Time) {
	propID := uuid.New()
	roomTypeID := uuid.New()
	ownerID := uuid.New()

	today := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, 10)
	checkIn := today
	checkOut := today.AddDate(0, 0, 1)

	// 1. Seed Owner
	_, err := pool.Exec(ctx, `
		INSERT INTO identity.app_user (id, phone_number, password_hash, full_name, role, is_active)
		VALUES ($1, $2, 'hash', 'Test Owner', 'HOTEL_OWNER', true)
		ON CONFLICT DO NOTHING;
	`, ownerID, "+251911"+propID.String()[:6])
	if err != nil {
		t.Fatalf("seed owner: %v", err)
	}

	// 2. Seed Property
	_, err = pool.Exec(ctx, `
		INSERT INTO property.property (id, code, name, category, status, city, region, owner_user_id)
		VALUES ($1, $2, 'Concurrency Test Hotel', 'BRANDED', 'ACTIVE', 'Addis Ababa', 'Addis Ababa', $3)
		ON CONFLICT DO NOTHING;
	`, propID, "PROP-C-"+propID.String()[:6], ownerID)
	if err != nil {
		t.Fatalf("seed property: %v", err)
	}

	// 3. Seed Room Type
	_, err = pool.Exec(ctx, `
		INSERT INTO property.room_type (id, property_id, code, name, capacity, base_rate_minor, total_inventory)
		VALUES ($1, $2, $3, 'Standard Room', 2, 450000, $4)
		ON CONFLICT DO NOTHING;
	`, roomTypeID, propID, "RT-C-"+roomTypeID.String()[:6], totalUnits)
	if err != nil {
		t.Fatalf("seed room type: %v", err)
	}

	// 4. Seed Daily Allocation with exact capacity
	_, err = pool.Exec(ctx, `
		INSERT INTO inventory.daily_allocations (
			id, property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
		) VALUES (
			gen_random_uuid(), $1, $2, $3, $4, 0, 0, 450000
		)
		ON CONFLICT (property_id, room_type_id, stay_date) DO UPDATE
		SET total_units = EXCLUDED.total_units, allocated_count = 0, blocked_count = 0;
	`, propID, roomTypeID, checkIn.Format("2006-01-02"), totalUnits)
	if err != nil {
		t.Fatalf("seed daily allocation: %v", err)
	}

	return propID, roomTypeID, checkIn, checkOut
}

// Scenario 1: Oversell Prevention under Burst Load (50 Concurrent Requests for 5 Total Units)
func TestConcurrency_OversellPrevention_BurstLoad(t *testing.T) {
	pool, rdb, router, _, _ := setupConcurrencyStack(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	totalUnits := 5
	concurrentRequests := 50

	propID, roomTypeID, checkIn, checkOut := seedConcurrencyProperty(ctx, t, pool, totalUnits)

	// Clean Redis hold keys for this property
	rdb.FlushDB(ctx)

	var successCount int64
	var conflictCount int64
	var otherErrorCount int64

	startBarrier := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(concurrentRequests)

	for i := 0; i < concurrentRequests; i++ {
		guestPhone := fmt.Sprintf("+251911%06d", i)
		guestName := fmt.Sprintf("Guest %d", i)

		go func(gPhone, gName string) {
			defer wg.Done()

			// Block until all goroutines are spawned to simulate a single burst millisecond
			<-startBarrier

			holdPayload := dto.CreateHoldRequest{
				PropertyID: propID.String(),
				RoomTypeID: roomTypeID.String(),
				CheckIn:    checkIn.Format("2006-01-02"),
				CheckOut:   checkOut.Format("2006-01-02"),
				GuestName:  gName,
				GuestPhone: gPhone,
			}

			body, _ := json.Marshal(holdPayload)
			req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			switch rec.Code {
			case http.StatusCreated:
				atomic.AddInt64(&successCount, 1)
			case http.StatusConflict:
				atomic.AddInt64(&conflictCount, 1)
			default:
				atomic.AddInt64(&otherErrorCount, 1)
			}
		}(guestPhone, guestName)
	}

	// Release the starting barrier
	close(startBarrier)
	wg.Wait()

	t.Logf("Burst concurrency results: %d successes (201), %d conflicts (409), %d other errors",
		successCount, conflictCount, otherErrorCount)

	if successCount != int64(totalUnits) {
		t.Fatalf("OVERBOOKING DETECTED! Expected exactly %d successes, got %d", totalUnits, successCount)
	}

	expectedConflicts := int64(concurrentRequests - totalUnits)
	if conflictCount != expectedConflicts {
		t.Fatalf("Expected exactly %d conflicts (409), got %d (other errors: %d)",
			expectedConflicts, conflictCount, otherErrorCount)
	}

	// Verify authoritative PostgreSQL state directly
	var dbAllocatedCount int
	var dbTotalUnits int
	err := pool.QueryRow(ctx, `
		SELECT allocated_count, total_units 
		FROM inventory.daily_allocations 
		WHERE property_id = $1 AND room_type_id = $2 AND stay_date = $3;
	`, propID, roomTypeID, checkIn.Format("2006-01-02")).Scan(&dbAllocatedCount, &dbTotalUnits)
	if err != nil {
		t.Fatalf("query db allocated count: %v", err)
	}

	if dbAllocatedCount != totalUnits {
		t.Errorf("DB state invariant violated: expected allocated_count = %d, got %d", totalUnits, dbAllocatedCount)
	}
	if dbAllocatedCount > dbTotalUnits {
		t.Fatalf("FATAL: chk_inventory_bounds hard constraint bypassed! allocated (%d) > total (%d)",
			dbAllocatedCount, dbTotalUnits)
	}
}

// Scenario 2: Two-Tier Conflict & Expiration Recovery
func TestConcurrency_TwoTierExpirationRecovery(t *testing.T) {
	pool, rdb, router, _, _ := setupConcurrencyStack(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	totalUnits := 2
	propID, roomTypeID, checkIn, checkOut := seedConcurrencyProperty(ctx, t, pool, totalUnits)
	rdb.FlushDB(ctx)

	// 1. Acquire 2 holds to fully exhaust capacity
	for i := 1; i <= 2; i++ {
		payload := dto.CreateHoldRequest{
			PropertyID: propID.String(),
			RoomTypeID: roomTypeID.String(),
			CheckIn:    checkIn.Format("2006-01-02"),
			CheckOut:   checkOut.Format("2006-01-02"),
			GuestName:  fmt.Sprintf("Primary Guest %d", i),
			GuestPhone: fmt.Sprintf("+25191200000%d", i),
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("initial hold %d failed with status %d", i, rec.Code)
		}
	}

	// 2. Third hold must fail with 409 Conflict
	rejectedPayload := dto.CreateHoldRequest{
		PropertyID: propID.String(),
		RoomTypeID: roomTypeID.String(),
		CheckIn:    checkIn.Format("2006-01-02"),
		CheckOut:   checkOut.Format("2006-01-02"),
		GuestName:  "Blocked Guest",
		GuestPhone: "+251912000099",
	}
	rejBody, _ := json.Marshal(rejectedPayload)
	rejReq := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(rejBody))
	rejReq.Header.Set("Content-Type", "application/json")
	rejRec := httptest.NewRecorder()
	router.ServeHTTP(rejRec, rejReq)

	if rejRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict when exhausted, got %d", rejRec.Code)
	}

	// 3. Simulate hold cancellation/expiry by clearing Redis and rolling back 1 unit in DB
	rdb.FlushDB(ctx)
	_, err := pool.Exec(ctx, `
		UPDATE inventory.daily_allocations 
		SET allocated_count = allocated_count - 1 
		WHERE property_id = $1 AND room_type_id = $2 AND stay_date = $3;
	`, propID, roomTypeID, checkIn.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("release 1 unit in db: %v", err)
	}

	// 4. Now the next hold request must succeed immediately
	recoveryReq := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(rejBody))
	recoveryReq.Header.Set("Content-Type", "application/json")
	recoveryRec := httptest.NewRecorder()
	router.ServeHTTP(recoveryRec, recoveryReq)

	if recoveryRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created after capacity recovery, got %d", recoveryRec.Code)
	}
}

// Scenario 3: Deadlock Prevention under Overlapping Cross-Date Bookings (ORDER BY stay_date ASC)
func TestConcurrency_DeadlockPrevention_CrossDateBookings(t *testing.T) {
	pool, rdb, _, _, invRepo := setupConcurrencyStack(t)
	defer pool.Close()
	defer rdb.Close()

	ctx := context.Background()
	propID := uuid.New()
	roomTypeID := uuid.New()
	ownerID := uuid.New()
	totalUnits := 100

	today := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, 30)

	// Seed property and 10 consecutive daily allocation slices
	_, _ = pool.Exec(ctx, `
		INSERT INTO identity.app_user (id, phone_number, password_hash, full_name, role, is_active)
		VALUES ($1, $2, 'hash', 'Owner Deadlock', 'HOTEL_OWNER', true)
		ON CONFLICT DO NOTHING;
	`, ownerID, "+251913"+propID.String()[:6])

	_, _ = pool.Exec(ctx, `
		INSERT INTO property.property (id, code, name, category, status, city, region, owner_user_id)
		VALUES ($1, $2, 'Deadlock Hotel', 'BRANDED', 'ACTIVE', 'Addis Ababa', 'Addis Ababa', $3)
		ON CONFLICT DO NOTHING;
	`, propID, "PROP-DL-"+propID.String()[:6], ownerID)

	_, _ = pool.Exec(ctx, `
		INSERT INTO property.room_type (id, property_id, code, name, capacity, base_rate_minor, total_inventory)
		VALUES ($1, $2, $3, 'Executive Suite', 2, 700000, $4)
		ON CONFLICT DO NOTHING;
	`, roomTypeID, propID, "RT-DL-"+roomTypeID.String()[:6], totalUnits)

	for i := 0; i < 10; i++ {
		stayDate := today.AddDate(0, 0, i)
		_, err := pool.Exec(ctx, `
			INSERT INTO inventory.daily_allocations (
				id, property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
			) VALUES (
				gen_random_uuid(), $1, $2, $3, $4, 0, 0, 700000
			)
			ON CONFLICT (property_id, room_type_id, stay_date) DO NOTHING;
		`, propID, roomTypeID, stayDate.Format("2006-01-02"), totalUnits)
		if err != nil {
			t.Fatalf("seed date slice %d: %v", i, err)
		}
	}

	// Concurrently execute 30 overlapping multi-night booking transactions:
	// Booking Type A: Days 0 -> 4
	// Booking Type B: Days 2 -> 6
	// Booking Type C: Days 4 -> 8
	var deadlockErrors int64
	var successfulCommits int64
	var wg sync.WaitGroup
	concurrentTransactions := 30
	wg.Add(concurrentTransactions)

	barrier := make(chan struct{})

	for i := 0; i < concurrentTransactions; i++ {
		offset := (i % 3) * 2 // 0, 2, 4
		inDate := today.AddDate(0, 0, offset)
		outDate := inDate.AddDate(0, 0, 4)

		go func(checkIn, checkOut time.Time) {
			defer wg.Done()
			<-barrier

			err := invRepo.CommitAllocation(context.Background(), propID, roomTypeID, checkIn, checkOut)
			if err != nil {
				if err.Error() == "ERROR: deadlock detected (SQLSTATE 40P01)" {
					atomic.AddInt64(&deadlockErrors, 1)
				}
			} else {
				atomic.AddInt64(&successfulCommits, 1)
			}
		}(inDate, outDate)
	}

	close(barrier)
	wg.Wait()

	t.Logf("Overlapping booking concurrency: %d successful commits, %d deadlocks",
		successfulCommits, deadlockErrors)

	if deadlockErrors > 0 {
		t.Fatalf("DEADLOCK DETECTED (SQLSTATE 40P01): %d deadlocks occurred! ORDER BY stay_date ASC lock order failed.", deadlockErrors)
	}

	if successfulCommits != int64(concurrentTransactions) {
		t.Fatalf("Expected all %d overlapping transactions to commit cleanly, got %d",
			concurrentTransactions, successfulCommits)
	}
}
