package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
)

const defaultHoldTTL = 10 * time.Minute

// HoldService orchestrates two-tier inventory hold acquisition and aggregate persistence.
type HoldService struct {
	inventory   domain.InventoryRepository
	reservation domain.ReservationRepository
}

// NewHoldService initializes a new HoldService instance.
func NewHoldService(inv domain.InventoryRepository, res domain.ReservationRepository) *HoldService {
	return &HoldService{
		inventory:   inv,
		reservation: res,
	}
}

// CreateHoldRequest contains required parameters to acquire a room hold.
type CreateHoldRequest struct {
	PropertyID uuid.UUID
	RoomTypeID uuid.UUID
	CheckIn    time.Time
	CheckOut   time.Time
	GuestName  string
	GuestPhone string
}

// CreateHoldResult contains the output of a successful inventory hold reservation.
type CreateHoldResult struct {
	ReservationID uuid.UUID
	HoldToken     string
	ExpiresAt     time.Time
}

// CreateHold acquires the fast-path Redis hold, then persists the
// reservation row. If persistence fails, the Redis hold is released
// deterministically before the error is returned — no capacity is ever
// left decremented without a corresponding reservation record.
func (s *HoldService) CreateHold(ctx context.Context, req CreateHoldRequest) (*CreateHoldResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !req.CheckOut.After(req.CheckIn) {
		return nil, fmt.Errorf("inventory: check-out must be after check-in")
	}

	token, expiresAt, err := s.inventory.AcquireHold(ctx, req.PropertyID, req.RoomTypeID, req.CheckIn, req.CheckOut, defaultHoldTTL)
	if err != nil {
		if errors.Is(err, domain.ErrInventoryUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("inventory: acquire hold: %w", err)
	}

	hold := &domain.InventoryHold{
		Token:      token,
		PropertyID: req.PropertyID,
		RoomTypeID: req.RoomTypeID,
		CheckIn:    req.CheckIn,
		CheckOut:   req.CheckOut,
		ExpiresAt:  expiresAt,
	}

	reservationID, err := s.reservation.CreateHold(ctx, hold, req.GuestName, req.GuestPhone)
	if err != nil {
		// Compensating action: the Redis hold must not outlive a failed
		// reservation write. Release runs with a fresh context so a
		// cancelled/timed-out parent context doesn't also kill the cleanup.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if releaseErr := s.inventory.ReleaseHold(releaseCtx, token); releaseErr != nil {
			return nil, fmt.Errorf("inventory: create reservation failed (%v) and hold release failed (%v) — requires manual reconciliation, token=%s", err, releaseErr, token)
		}
		return nil, fmt.Errorf("inventory: create reservation: %w", err)
	}

	return &CreateHoldResult{
		ReservationID: reservationID,
		HoldToken:     token,
		ExpiresAt:     expiresAt,
	}, nil
}
