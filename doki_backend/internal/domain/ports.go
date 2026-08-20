package domain

import (
	"context"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/types"
)

// ReservationRepository persists and retrieves the reservation aggregate.
// Implemented by internal/adapter/repository/postgres; never called directly by HTTP handlers.
type ReservationRepository interface {
	CreateHold(ctx context.Context, hold *InventoryHold, guestName, guestPhone string) (reservationID uuid.UUID, err error)
	GetByID(ctx context.Context, id uuid.UUID) (*Reservation, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
}

// InventoryRepository is the sole entry point for both tiers of the locking engine.
// Callers never touch Redis or Postgres directly.
type InventoryRepository interface {
	// AcquireHold executes Layer 1 (Redis) for every date in the stay range.
	// Returns ErrInventoryUnavailable if any date is exhausted.
	AcquireHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (token string, expiresAt time.Time, err error)

	// ReleaseHold undoes AcquireHold — called on explicit cancellation,
	// hold expiry, or as compensation when a later step fails.
	ReleaseHold(ctx context.Context, token string) error

	// CommitAllocation executes Layer 2 (Postgres) inside a single transaction,
	// re-validating capacity independently of Redis's answer.
	CommitAllocation(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error
}

// PaymentGateway is implemented once per provider (Telebirr, CBE Birr, Chapa)
// in internal/adapter/integration/payment; the domain only depends on this interface.
type PaymentGateway interface {
	InitiateCharge(ctx context.Context, reservationRef string, amount types.Money, payerPhone string) (redirectURL string, err error)
	VerifyWebhookSignature(payload []byte, signature string) bool
}
