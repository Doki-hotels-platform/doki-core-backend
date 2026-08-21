package inventory

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
	"doki-backend/internal/platform/logger"
)

type mockSweeperResRepo struct {
	mu           sync.Mutex
	reservations map[uuid.UUID]*domain.Reservation
}

func newMockSweeperResRepo() *mockSweeperResRepo {
	return &mockSweeperResRepo{
		reservations: make(map[uuid.UUID]*domain.Reservation),
	}
}

func (m *mockSweeperResRepo) CreateHold(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (m *mockSweeperResRepo) CreateReservation(ctx context.Context, res *domain.Reservation) error {
	return nil
}
func (m *mockSweeperResRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res, ok := m.reservations[id]
	if !ok {
		return nil, domain.ErrReservationNotFound
	}
	return res, nil
}
func (m *mockSweeperResRepo) GetByReference(ctx context.Context, ref string) (*domain.Reservation, error) {
	return nil, nil
}
func (m *mockSweeperResRepo) UpdateStatus(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error {
	return nil
}

func (m *mockSweeperResRepo) GetExpiredHolds(ctx context.Context, cutoff time.Time, limit int) ([]*domain.Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var results []*domain.Reservation
	for _, res := range m.reservations {
		if res.Status == "INVENTORY_HOLD" && res.HoldExpiresAt != nil && !res.HoldExpiresAt.After(cutoff) {
			results = append(results, res)
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (m *mockSweeperResRepo) MarkReservationExpired(ctx context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	res, ok := m.reservations[id]
	if !ok {
		return domain.ErrReservationNotFound
	}
	res.Status = "EXPIRED"
	res.UpdatedAt = time.Now().UTC()
	return nil
}

type mockSweeperFastHoldPort struct {
	mu           sync.Mutex
	released     []string
	releaseErrFn func(token string) error
}

func (m *mockSweeperFastHoldPort) AcquireFastHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
	return "", time.Time{}, nil
}

func (m *mockSweeperFastHoldPort) ReleaseFastHold(ctx context.Context, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.released = append(m.released, token)
	if m.releaseErrFn != nil {
		return m.releaseErrFn(token)
	}
	return nil
}

func TestSweepExpiredHolds_ReconcilesPostgresAndRedis(t *testing.T) {
	resRepo := newMockSweeperResRepo()
	redisHold := &mockSweeperFastHoldPort{}
	log := logger.New(logger.Config{Output: io.Discard})

	sweeper := NewHoldSweeperService(resRepo, redisHold, log)

	// 1. Seed 2 expired holds and 1 active hold
	pastTime := time.Now().UTC().Add(-5 * time.Minute)
	futureTime := time.Now().UTC().Add(10 * time.Minute)

	token1 := "hold:tok:expired-1"
	token2 := "hold:tok:expired-2"
	token3 := "hold:tok:active-3"

	res1ID := uuid.New()
	res2ID := uuid.New()
	res3ID := uuid.New()

	resRepo.reservations[res1ID] = &domain.Reservation{
		ID:            res1ID,
		Status:        "INVENTORY_HOLD",
		HoldToken:     &token1,
		HoldExpiresAt: &pastTime,
	}

	resRepo.reservations[res2ID] = &domain.Reservation{
		ID:            res2ID,
		Status:        "INVENTORY_HOLD",
		HoldToken:     &token2,
		HoldExpiresAt: &pastTime,
	}

	resRepo.reservations[res3ID] = &domain.Reservation{
		ID:            res3ID,
		Status:        "INVENTORY_HOLD",
		HoldToken:     &token3,
		HoldExpiresAt: &futureTime,
	}

	// 2. Execute sweep
	swept, err := sweeper.SweepExpiredHolds(context.Background(), 50)
	if err != nil {
		t.Fatalf("sweep failed: %v", err)
	}

	if swept != 2 {
		t.Errorf("expected 2 expired holds swept, got %d", swept)
	}

	// 3. Verify Redis released both expired tokens
	if len(redisHold.released) != 2 {
		t.Errorf("expected 2 Redis tokens released, got %d", len(redisHold.released))
	}

	// 4. Verify PostgreSQL statuses
	if resRepo.reservations[res1ID].Status != "EXPIRED" {
		t.Errorf("expected res1 to be EXPIRED, got %s", resRepo.reservations[res1ID].Status)
	}
	if resRepo.reservations[res2ID].Status != "EXPIRED" {
		t.Errorf("expected res2 to be EXPIRED, got %s", resRepo.reservations[res2ID].Status)
	}
	if resRepo.reservations[res3ID].Status != "INVENTORY_HOLD" {
		t.Errorf("expected active res3 to remain INVENTORY_HOLD, got %s", resRepo.reservations[res3ID].Status)
	}
}

func TestSweepExpiredHolds_IgnoresActiveHolds(t *testing.T) {
	resRepo := newMockSweeperResRepo()
	redisHold := &mockSweeperFastHoldPort{}
	log := logger.New(logger.Config{Output: io.Discard})

	sweeper := NewHoldSweeperService(resRepo, redisHold, log)

	futureTime := time.Now().UTC().Add(10 * time.Minute)
	token := "hold:tok:active"
	resID := uuid.New()

	resRepo.reservations[resID] = &domain.Reservation{
		ID:            resID,
		Status:        "INVENTORY_HOLD",
		HoldToken:     &token,
		HoldExpiresAt: &futureTime,
	}

	swept, err := sweeper.SweepExpiredHolds(context.Background(), 10)
	if err != nil {
		t.Fatalf("sweep error: %v", err)
	}

	if swept != 0 {
		t.Errorf("expected 0 swept holds, got %d", swept)
	}

	if len(redisHold.released) != 0 {
		t.Errorf("expected 0 Redis releases, got %d", len(redisHold.released))
	}
}

func TestSweepExpiredHolds_HandlesRedisTokenAlreadyGone(t *testing.T) {
	resRepo := newMockSweeperResRepo()
	redisHold := &mockSweeperFastHoldPort{
		releaseErrFn: func(token string) error {
			return errors.New("redis: key does not exist (already TTL expired)")
		},
	}
	log := logger.New(logger.Config{Output: io.Discard})

	sweeper := NewHoldSweeperService(resRepo, redisHold, log)

	pastTime := time.Now().UTC().Add(-20 * time.Minute)
	token := "hold:tok:already-gone"
	resID := uuid.New()

	resRepo.reservations[resID] = &domain.Reservation{
		ID:            resID,
		Status:        "INVENTORY_HOLD",
		HoldToken:     &token,
		HoldExpiresAt: &pastTime,
	}

	swept, err := sweeper.SweepExpiredHolds(context.Background(), 10)
	if err != nil {
		t.Fatalf("expected graceful handling of Redis errors, got: %v", err)
	}

	if swept != 1 {
		t.Errorf("expected 1 hold marked expired, got %d", swept)
	}

	if resRepo.reservations[resID].Status != "EXPIRED" {
		t.Errorf("expected Postgres status EXPIRED, got %s", resRepo.reservations[resID].Status)
	}
}
