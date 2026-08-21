package redis

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"doki-backend/internal/domain"
	"doki-backend/internal/platform/cache"
)

func getTestRedisClient(t *testing.T) *redis.Client {
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = os.Getenv("REDIS_ADDR")
	}
	if addr == "" {
		addr = "localhost:6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := cache.NewRedisClient(ctx, cache.DefaultRedisConfig(addr, "", 0))
	if err != nil {
		t.Skipf("skipping Redis integration test (Redis unreachable at %s: %v)", addr, err)
	}

	return client
}

func TestInventoryHoldAdapter_AcquireAndRelease(t *testing.T) {
	client := getTestRedisClient(t)
	defer client.Close()

	ctx := context.Background()
	adapter, err := NewInventoryHoldAdapter(client)
	if err != nil {
		t.Fatalf("failed to initialize adapter: %v", err)
	}

	propID := uuid.New()
	roomTypeID := uuid.New()
	checkIn := time.Now().UTC().Add(24 * time.Hour)
	checkOut := checkIn.Add(3 * 24 * time.Hour) // 3 nights

	// Total capacity = 2
	token1, _, err := adapter.AcquireFastHold(ctx, propID, roomTypeID, checkIn, checkOut, 2, 5*time.Minute)
	if err != nil {
		t.Fatalf("first hold acquisition should succeed: %v", err)
	}

	token2, _, err := adapter.AcquireFastHold(ctx, propID, roomTypeID, checkIn, checkOut, 2, 5*time.Minute)
	if err != nil {
		t.Fatalf("second hold acquisition should succeed: %v", err)
	}

	// Third attempt should fail because capacity is exhausted (2 of 2 held)
	_, _, err = adapter.AcquireFastHold(ctx, propID, roomTypeID, checkIn, checkOut, 2, 5*time.Minute)
	if !errors.Is(err, domain.ErrInventoryUnavailable) {
		t.Fatalf("expected ErrInventoryUnavailable on exhausted capacity, got: %v", err)
	}

	// Release token1 -> frees up 1 unit
	err = adapter.ReleaseFastHold(ctx, token1)
	if err != nil {
		t.Fatalf("failed to release hold: %v", err)
	}

	// Now third hold can succeed
	token3, _, err := adapter.AcquireFastHold(ctx, propID, roomTypeID, checkIn, checkOut, 2, 5*time.Minute)
	if err != nil {
		t.Fatalf("hold after release should succeed: %v", err)
	}

	// Cleanup remaining holds
	_ = adapter.ReleaseFastHold(ctx, token2)
	_ = adapter.ReleaseFastHold(ctx, token3)
}
