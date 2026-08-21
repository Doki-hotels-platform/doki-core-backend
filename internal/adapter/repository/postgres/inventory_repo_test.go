package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/domain"
	"doki-backend/internal/platform/database"
)

func getTestPool(t *testing.T) *pgxpool.Pool {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://doki:doki_secret@localhost:5432/doki_db?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, dsn)
	if err != nil {
		t.Skipf("skipping PostgreSQL integration test (database unreachable: %v)", err)
	}

	return pool
}

func setupTestFixture(ctx context.Context, t *testing.T, pool *pgxpool.Pool, totalUnits int) (uuid.UUID, uuid.UUID, time.Time, time.Time) {
	propID := uuid.New()
	roomTypeID := uuid.New()
	ownerID := uuid.New()

	// 1. Insert Owner
	_, err := pool.Exec(ctx, `
		INSERT INTO identity.app_user (id, phone_number, full_name, role)
		VALUES ($1, $2, 'Test Owner', 'HOTEL_OWNER')
		ON CONFLICT (phone_number) DO NOTHING;
	`, ownerID, "+251911"+uuid.New().String()[:6])
	if err != nil {
		t.Fatalf("failed to setup owner: %v", err)
	}

	// 2. Insert Property
	_, err = pool.Exec(ctx, `
		INSERT INTO property.property (id, code, name, address, city, region, owner_id)
		VALUES ($1, $2, 'Test Concurrency Hotel', 'Bole Rd', 'Addis Ababa', 'Addis Ababa', $3)
		ON CONFLICT (code) DO NOTHING;
	`, propID, "PROP-"+uuid.New().String()[:8], ownerID)
	if err != nil {
		t.Fatalf("failed to setup property: %v", err)
	}

	// 3. Insert Room Type
	_, err = pool.Exec(ctx, `
		INSERT INTO property.room_type (id, property_id, code, name, capacity, base_rate_minor, total_inventory)
		VALUES ($1, $2, $3, 'Deluxe Suite', 2, 500000, $4);
	`, roomTypeID, propID, "RT-"+uuid.New().String()[:6], totalUnits)
	if err != nil {
		t.Fatalf("failed to setup room type: %v", err)
	}

	startDate := time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	endDate := startDate.Add(5 * 24 * time.Hour)

	// 4. Insert Daily Allocations for 5 contiguous days
	for d := startDate; d.Before(endDate); d = d.Add(24 * time.Hour) {
		_, err := pool.Exec(ctx, `
			INSERT INTO inventory.daily_allocations (
				property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
			) VALUES ($1, $2, $3, $4, 0, 0, 500000);
		`, propID, roomTypeID, d.Format("2006-01-02"), totalUnits)
		if err != nil {
			t.Fatalf("failed to insert daily allocation for %v: %v", d, err)
		}
	}

	return propID, roomTypeID, startDate, endDate
}

func TestCommitAllocation_Success(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	propID, roomTypeID, startDate, _ := setupTestFixture(ctx, t, pool, 5)

	repo := NewInventoryRepository(pool)

	checkIn := startDate
	checkOut := startDate.Add(2 * 24 * time.Hour) // 2 nights

	err := repo.CommitAllocation(ctx, propID, roomTypeID, checkIn, checkOut)
	if err != nil {
		t.Fatalf("expected commit allocation to succeed, got: %v", err)
	}

	// Verify allocated_count incremented to 1 for both nights
	allocs, err := repo.GetDailyAllocations(ctx, propID, roomTypeID, checkIn, checkOut)
	if err != nil {
		t.Fatalf("failed to get daily allocations: %v", err)
	}

	if len(allocs) != 2 {
		t.Fatalf("expected 2 allocation records, got %d", len(allocs))
	}

	for _, a := range allocs {
		if a.AllocatedCount != 1 {
			t.Errorf("expected allocated_count=1, got %d for date %v", a.AllocatedCount, a.StayDate)
		}
	}
}

func TestCommitAllocation_ExceedCapacity_Rejection(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	// Capacity = 1 unit
	propID, roomTypeID, startDate, _ := setupTestFixture(ctx, t, pool, 1)

	repo := NewInventoryRepository(pool)

	checkIn := startDate
	checkOut := startDate.Add(2 * 24 * time.Hour)

	// First booking consumes capacity
	err := repo.CommitAllocation(ctx, propID, roomTypeID, checkIn, checkOut)
	if err != nil {
		t.Fatalf("first allocation should succeed, got: %v", err)
	}

	// Second booking should fail with ErrInventoryUnavailable
	err = repo.CommitAllocation(ctx, propID, roomTypeID, checkIn, checkOut)
	if !errors.Is(err, domain.ErrInventoryUnavailable) {
		t.Fatalf("expected domain.ErrInventoryUnavailable, got: %v", err)
	}
}

func TestDeadlockPrevention_OrderedLocks(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	propID, roomTypeID, startDate, _ := setupTestFixture(ctx, t, pool, 10)

	repo := NewInventoryRepository(pool)

	// Two overlapping booking ranges
	// Booking 1: Night 1 to Night 4
	// Booking 2: Night 2 to Night 5
	b1CheckIn := startDate
	b1CheckOut := startDate.Add(3 * 24 * time.Hour)

	b2CheckIn := startDate.Add(1 * 24 * time.Hour)
	b2CheckOut := startDate.Add(4 * 24 * time.Hour)

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(2)

	go func() {
		defer wg.Done()
		errs <- repo.CommitAllocation(ctx, propID, roomTypeID, b1CheckIn, b1CheckOut)
	}()

	go func() {
		defer wg.Done()
		errs <- repo.CommitAllocation(ctx, propID, roomTypeID, b2CheckIn, b2CheckOut)
	}()

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent overlapping allocation failed: %v", err)
		}
	}
}
