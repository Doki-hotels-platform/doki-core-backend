package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"doki-backend/internal/platform/cache"
)

func getTestRedis(t *testing.T) *redis.Client {
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
		t.Skipf("skipping Redis idempotency test (Redis unreachable: %v)", err)
	}

	return client
}

func TestIdempotencyMiddleware_ReplaysCachedResponse(t *testing.T) {
	rdb := getTestRedis(t)
	defer rdb.Close()

	var executionCount int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&executionCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"reservation_id":"res-123","status":"HOLD"}`))
	})

	mw := IdempotencyMiddleware(rdb)(handler)

	idempKey := uuid.New().String()

	// 1. First execution -> executes handler
	req1 := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", nil)
	req1.Header.Set(IdempotencyHeader, idempKey)
	rec1 := httptest.NewRecorder()
	mw.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request expected 201 Created, got %d", rec1.Code)
	}
	if count := atomic.LoadInt32(&executionCount); count != 1 {
		t.Fatalf("expected handler to execute exactly 1 time, got %d", count)
	}

	// 2. Second execution with same key -> replayed from Redis cache without executing handler again
	req2 := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", nil)
	req2.Header.Set(IdempotencyHeader, idempKey)
	rec2 := httptest.NewRecorder()
	mw.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusCreated {
		t.Fatalf("second request expected 201 Created from cache, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-Cache") != "HIT-IDEMPOTENT" {
		t.Errorf("expected X-Cache header HIT-IDEMPOTENT, got %s", rec2.Header().Get("X-Cache"))
	}
	if count := atomic.LoadInt32(&executionCount); count != 1 {
		t.Errorf("expected handler execution count to remain 1, got %d", count)
	}
}
