package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
)

type mockInventoryRepo struct {
	acquireHoldFn func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (string, time.Time, error)
	releaseHoldFn func(ctx context.Context, token string) error
	commitAllocFn func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error
}

func (m *mockInventoryRepo) AcquireHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (string, time.Time, error) {
	if m.acquireHoldFn != nil {
		return m.acquireHoldFn(ctx, propertyID, roomTypeID, checkIn, checkOut, ttl)
	}
	return "test-token-123", time.Now().Add(10 * time.Minute), nil
}

func (m *mockInventoryRepo) ReleaseHold(ctx context.Context, token string) error {
	if m.releaseHoldFn != nil {
		return m.releaseHoldFn(ctx, token)
	}
	return nil
}

func (m *mockInventoryRepo) CommitAllocation(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error {
	if m.commitAllocFn != nil {
		return m.commitAllocFn(ctx, propertyID, roomTypeID, checkIn, checkOut)
	}
	return nil
}

type mockReservationRepo struct {
	createHoldFn func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error)
	getByIDFn    func(ctx context.Context, id uuid.UUID) (*domain.Reservation, error)
	updateStatus func(ctx context.Context, id uuid.UUID, status string) error
}

func (m *mockReservationRepo) CreateHold(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
	if m.createHoldFn != nil {
		return m.createHoldFn(ctx, hold, guestName, guestPhone)
	}
	return uuid.New(), nil
}

func (m *mockReservationRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockReservationRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	if m.updateStatus != nil {
		return m.updateStatus(ctx, id, status)
	}
	return nil
}

func TestHoldService_CreateHold_RollbackOnPostgresFailure(t *testing.T) {
	var releaseCalled bool
	var releasedToken string

	mockInv := &mockInventoryRepo{
		acquireHoldFn: func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (string, time.Time, error) {
			return "hold-token-abc", time.Now().Add(10 * time.Minute), nil
		},
		releaseHoldFn: func(ctx context.Context, token string) error {
			releaseCalled = true
			releasedToken = token
			return nil
		},
	}

	mockRes := &mockReservationRepo{
		createHoldFn: func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
			return uuid.Nil, errors.New("db connection lost")
		},
	}

	service := NewHoldService(mockInv, mockRes)

	req := CreateHoldRequest{
		PropertyID: uuid.New(),
		RoomTypeID: uuid.New(),
		CheckIn:    time.Now().Add(24 * time.Hour),
		CheckOut:   time.Now().Add(48 * time.Hour),
		GuestName:  "Abebe Bikila",
		GuestPhone: "+251911223344",
	}

	_, err := service.CreateHold(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from create hold, got nil")
	}

	if !releaseCalled {
		t.Error("expected ReleaseHold compensating transaction to be called when DB persistence fails")
	}

	if releasedToken != "hold-token-abc" {
		t.Errorf("expected released token 'hold-token-abc', got '%s'", releasedToken)
	}
}

func TestHoldService_CreateHold_Success(t *testing.T) {
	mockInv := &mockInventoryRepo{}
	mockRes := &mockReservationRepo{}

	service := NewHoldService(mockInv, mockRes)

	req := CreateHoldRequest{
		PropertyID: uuid.New(),
		RoomTypeID: uuid.New(),
		CheckIn:    time.Now().Add(24 * time.Hour),
		CheckOut:   time.Now().Add(48 * time.Hour),
		GuestName:  "Abebe Bikila",
		GuestPhone: "+251911223344",
	}

	res, err := service.CreateHold(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.ReservationID == uuid.Nil {
		t.Error("expected valid reservation ID, got Nil")
	}
	if res.HoldToken == "" {
		t.Error("expected non-empty hold token")
	}
}
