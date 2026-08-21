package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
)

func TestReservationRepository_Lifecycle(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx := context.Background()
	propID, roomTypeID, startDate, endDate := setupTestFixture(ctx, t, pool, 5)

	repo := NewReservationRepository(pool)

	// 1. Create Hold
	hold := &domain.InventoryHold{
		Token:      "test-token-" + uuid.New().String()[:8],
		PropertyID: propID,
		RoomTypeID: roomTypeID,
		CheckIn:    startDate,
		CheckOut:   endDate,
		ExpiresAt:  time.Now().UTC().Add(10 * time.Minute),
	}

	resID, err := repo.CreateHold(ctx, hold, "Abebe Bikila", "+251911223344")
	if err != nil {
		t.Fatalf("failed to create hold: %v", err)
	}

	if resID == uuid.Nil {
		t.Fatal("expected valid reservation UUID, got Nil")
	}

	// 2. Get By ID
	res, err := repo.GetByID(ctx, resID)
	if err != nil {
		t.Fatalf("failed to get reservation by id: %v", err)
	}

	if res.GuestName != "Abebe Bikila" {
		t.Errorf("expected guest name 'Abebe Bikila', got '%s'", res.GuestName)
	}
	if res.Status != "INVENTORY_HOLD" {
		t.Errorf("expected status 'INVENTORY_HOLD', got '%s'", res.Status)
	}

	// 3. Get By Reference
	resByRef, err := repo.GetByReference(ctx, res.BookingReference)
	if err != nil {
		t.Fatalf("failed to get reservation by ref: %v", err)
	}
	if resByRef.ID != res.ID {
		t.Errorf("expected id %v, got %v", res.ID, resByRef.ID)
	}

	// 4. Update Status: INVENTORY_HOLD -> CONFIRMED
	err = repo.UpdateStatus(ctx, resID, "INVENTORY_HOLD", "CONFIRMED")
	if err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	updatedRes, err := repo.GetByID(ctx, resID)
	if err != nil {
		t.Fatalf("failed to get updated reservation: %v", err)
	}
	if updatedRes.Status != "CONFIRMED" {
		t.Errorf("expected status 'CONFIRMED', got '%s'", updatedRes.Status)
	}

	// 5. Invalid transition guard
	err = repo.UpdateStatus(ctx, resID, "INVENTORY_HOLD", "CHECKED_IN")
	if !errors.Is(err, domain.ErrInvalidStatusChange) {
		t.Errorf("expected ErrInvalidStatusChange on mismatched oldStatus, got: %v", err)
	}
}
