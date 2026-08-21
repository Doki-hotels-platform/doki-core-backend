package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
)

type mockFastHoldAdapter struct {
	acquireFn func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error)
	releaseFn func(ctx context.Context, token string) error
}

func (m *mockFastHoldAdapter) AcquireFastHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
	if m.acquireFn != nil {
		return m.acquireFn(ctx, propertyID, roomTypeID, checkIn, checkOut, totalCapacity, ttl)
	}
	return "hold:tok:test-123", time.Now().Add(15 * time.Minute), nil
}

func (m *mockFastHoldAdapter) ReleaseFastHold(ctx context.Context, token string) error {
	if m.releaseFn != nil {
		return m.releaseFn(ctx, token)
	}
	return nil
}

type mockReservationRepo struct {
	createHoldFn func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error)
	getByIDFn    func(ctx context.Context, id uuid.UUID) (*domain.Reservation, error)
	updateStatus func(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error
}

func (m *mockReservationRepo) CreateHold(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
	if m.createHoldFn != nil {
		return m.createHoldFn(ctx, hold, guestName, guestPhone)
	}
	return uuid.New(), nil
}

func (m *mockReservationRepo) CreateReservation(ctx context.Context, res *domain.Reservation) error {
	return nil
}

func (m *mockReservationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockReservationRepo) GetByReference(ctx context.Context, ref string) (*domain.Reservation, error) {
	return nil, nil
}

func (m *mockReservationRepo) GetExpiredHolds(ctx context.Context, cutoff time.Time, limit int) ([]*domain.Reservation, error) {
	return nil, nil
}

func (m *mockReservationRepo) MarkReservationExpired(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *mockReservationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error {
	if m.updateStatus != nil {
		return m.updateStatus(ctx, id, oldStatus, newStatus)
	}
	return nil
}

func TestCreateHold_Success(t *testing.T) {
	mockFast := &mockFastHoldAdapter{}
	mockRes := &mockReservationRepo{}

	service := NewHoldService(mockFast, mockRes)

	req := CreateHoldRequest{
		PropertyID:    uuid.New(),
		RoomTypeID:    uuid.New(),
		CheckIn:       time.Now().UTC().Add(24 * time.Hour),
		CheckOut:      time.Now().UTC().Add(48 * time.Hour),
		TotalCapacity: 10,
		GuestName:     "Abebe Bikila",
		GuestPhone:    "+251911223344",
	}

	res, err := service.CreateHold(context.Background(), req)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if res.ReservationID == uuid.Nil {
		t.Error("expected valid reservation UUID, got Nil")
	}
	if res.HoldToken == "" {
		t.Error("expected non-empty hold token")
	}
	if res.Status != "INVENTORY_HOLD" {
		t.Errorf("expected status 'INVENTORY_HOLD', got '%s'", res.Status)
	}
}

func TestCreateHold_RedisUnavailable_Rejection(t *testing.T) {
	var postgresCalled bool

	mockFast := &mockFastHoldAdapter{
		acquireFn: func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
			return "", time.Time{}, domain.ErrInventoryUnavailable
		},
	}

	mockRes := &mockReservationRepo{
		createHoldFn: func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
			postgresCalled = true
			return uuid.New(), nil
		},
	}

	service := NewHoldService(mockFast, mockRes)

	req := CreateHoldRequest{
		PropertyID:    uuid.New(),
		RoomTypeID:    uuid.New(),
		CheckIn:       time.Now().UTC().Add(24 * time.Hour),
		CheckOut:      time.Now().UTC().Add(48 * time.Hour),
		TotalCapacity: 10,
		GuestName:     "Abebe Bikila",
		GuestPhone:    "+251911223344",
	}

	_, err := service.CreateHold(context.Background(), req)
	if !errors.Is(err, domain.ErrInventoryUnavailable) {
		t.Fatalf("expected domain.ErrInventoryUnavailable, got: %v", err)
	}

	if postgresCalled {
		t.Error("PostgreSQL should NOT be touched if Redis capacity check fails")
	}
}

func TestCreateHold_PostgresFailure_CompensatesRedis(t *testing.T) {
	var releaseCalled bool
	var releasedToken string

	mockFast := &mockFastHoldAdapter{
		acquireFn: func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
			return "hold:tok:special-abc", time.Now().Add(15 * time.Minute), nil
		},
		releaseFn: func(ctx context.Context, token string) error {
			releaseCalled = true
			releasedToken = token
			return nil
		},
	}

	mockRes := &mockReservationRepo{
		createHoldFn: func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
			return uuid.Nil, errors.New("postgres connection timeout")
		},
	}

	service := NewHoldService(mockFast, mockRes)

	req := CreateHoldRequest{
		PropertyID:    uuid.New(),
		RoomTypeID:    uuid.New(),
		CheckIn:       time.Now().UTC().Add(24 * time.Hour),
		CheckOut:      time.Now().UTC().Add(48 * time.Hour),
		TotalCapacity: 10,
		GuestName:     "Abebe Bikila",
		GuestPhone:    "+251911223344",
	}

	_, err := service.CreateHold(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from create hold, got nil")
	}

	if !releaseCalled {
		t.Error("expected ReleaseFastHold compensation to be called when DB persistence fails")
	}

	if releasedToken != "hold:tok:special-abc" {
		t.Errorf("expected released token 'hold:tok:special-abc', got '%s'", releasedToken)
	}
}

func TestCreateHold_InvalidDateRange(t *testing.T) {
	mockFast := &mockFastHoldAdapter{}
	mockRes := &mockReservationRepo{}

	service := NewHoldService(mockFast, mockRes)

	// Case 1: checkOut before checkIn
	req1 := CreateHoldRequest{
		PropertyID: uuid.New(),
		RoomTypeID: uuid.New(),
		CheckIn:    time.Now().UTC().Add(48 * time.Hour),
		CheckOut:   time.Now().UTC().Add(24 * time.Hour),
	}
	_, err := service.CreateHold(context.Background(), req1)
	if !errors.Is(err, domain.ErrInvalidDateRange) {
		t.Errorf("expected ErrInvalidDateRange, got: %v", err)
	}

	// Case 2: checkIn in the past
	req2 := CreateHoldRequest{
		PropertyID: uuid.New(),
		RoomTypeID: uuid.New(),
		CheckIn:    time.Now().UTC().Add(-48 * time.Hour),
		CheckOut:   time.Now().UTC().Add(24 * time.Hour),
	}
	_, err = service.CreateHold(context.Background(), req2)
	if !errors.Is(err, domain.ErrInvalidDateRange) {
		t.Errorf("expected ErrInvalidDateRange for past dates, got: %v", err)
	}
}
