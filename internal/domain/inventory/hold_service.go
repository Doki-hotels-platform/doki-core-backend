package inventory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
)

const defaultHoldTTL = 15 * time.Minute

// HoldService coordinates Layer 1 Redis fast-path holds with Layer 2 PostgreSQL persistence.
type HoldService struct {
	fastHold        domain.FastHoldPort
	reservationRepo domain.ReservationRepository
}

// NewHoldService initializes a new HoldService instance.
func NewHoldService(fastHold domain.FastHoldPort, resRepo domain.ReservationRepository) *HoldService {
	return &HoldService{
		fastHold:        fastHold,
		reservationRepo: resRepo,
	}
}

// CreateHoldRequest contains required parameters to acquire a room hold.
type CreateHoldRequest struct {
	PropertyID    uuid.UUID
	RoomTypeID    uuid.UUID
	CheckIn       time.Time
	CheckOut      time.Time
	TotalCapacity int
	GuestName     string
	GuestPhone    string
}

// CreateHoldResult contains the output of a successful inventory hold reservation.
type CreateHoldResult struct {
	ReservationID uuid.UUID
	HoldToken     string
	ExpiresAt     time.Time
	Status        string
}

// CreateHold acquires the fast-path Redis hold, then persists the
// reservation row. If persistence fails or parent context cancels, the Redis hold is
// released deterministically before returning — no capacity is ever left decremented
// without a corresponding reservation record.
func (s *HoldService) CreateHold(ctx context.Context, req CreateHoldRequest) (*CreateHoldResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 1. Validate Date Range
	if !req.CheckOut.After(req.CheckIn) {
		return nil, domain.ErrInvalidDateRange
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	if req.CheckIn.Before(today) {
		return nil, domain.ErrInvalidDateRange
	}

	if req.TotalCapacity <= 0 {
		req.TotalCapacity = 100 // Safe default fallback
	}

	// 2. Layer 1: Acquire Redis Fast-Path Hold
	token, expiresAt, err := s.fastHold.AcquireFastHold(
		ctx,
		req.PropertyID,
		req.RoomTypeID,
		req.CheckIn,
		req.CheckOut,
		req.TotalCapacity,
		defaultHoldTTL,
	)
	if err != nil {
		if errors.Is(err, domain.ErrInventoryUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("fast hold acquisition: %w", err)
	}

	hold := &domain.InventoryHold{
		Token:      token,
		PropertyID: req.PropertyID,
		RoomTypeID: req.RoomTypeID,
		CheckIn:    req.CheckIn,
		CheckOut:   req.CheckOut,
		ExpiresAt:  expiresAt,
	}

	// 3. Layer 2: Persist preliminary reservation record in PostgreSQL
	reservationID, err := s.reservationRepo.CreateHold(ctx, hold, req.GuestName, req.GuestPhone)
	if err != nil {
		// 4. CRITICAL Compensation Path: Redis hold must not outlive a failed reservation write.
		// Release runs with an independent detached context so a cancelled/timed-out parent
		// context does not prevent the rollback.
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if releaseErr := s.fastHold.ReleaseFastHold(releaseCtx, token); releaseErr != nil {
			return nil, fmt.Errorf(
				"create reservation failed (%v) and fast hold release failed (%v) [CRITICAL: reconciliation required, token=%s]",
				err, releaseErr, token,
			)
		}
		return nil, fmt.Errorf("create reservation: %w", err)
	}

	return &CreateHoldResult{
		ReservationID: reservationID,
		HoldToken:     token,
		ExpiresAt:     expiresAt,
		Status:        "INVENTORY_HOLD",
	}, nil
}
