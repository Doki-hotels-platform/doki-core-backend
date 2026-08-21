package domain

import (
	"context"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain/identity"
	"doki-backend/internal/domain/property"
	"doki-backend/pkg/types"
)

// DailyAllocation represents a daily slice of inventory capacity.
type DailyAllocation struct {
	ID             uuid.UUID   `json:"id"`
	PropertyID     uuid.UUID   `json:"property_id"`
	RoomTypeID     uuid.UUID   `json:"room_type_id"`
	StayDate       time.Time   `json:"stay_date"`
	TotalUnits     int         `json:"total_units"`
	AllocatedCount int         `json:"allocated_count"`
	BlockedCount   int         `json:"blocked_count"`
	RateMinor      types.Money `json:"rate_minor"`
}

// PropertyFilter defines criteria for searching and filtering properties.
type PropertyFilter = property.PropertyFilter

// ReservationRepository persists and retrieves the reservation aggregate.
// Implemented by internal/adapter/repository/postgres; never called directly by HTTP handlers.
type ReservationRepository interface {
	CreateHold(ctx context.Context, hold *InventoryHold, guestName, guestPhone string) (reservationID uuid.UUID, err error)
	CreateReservation(ctx context.Context, res *Reservation) error
	GetByID(ctx context.Context, id uuid.UUID) (*Reservation, error)
	GetByReference(ctx context.Context, ref string) (*Reservation, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error
	GetExpiredHolds(ctx context.Context, cutoff time.Time, limit int) ([]*Reservation, error)
	MarkReservationExpired(ctx context.Context, id uuid.UUID) error
}

// InventoryRepository is the sole entry point for the two-tier locking engine.
type InventoryRepository interface {
	// AcquireHold executes Layer 1 (Redis) or Layer 2 capacity check.
	AcquireHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (token string, expiresAt time.Time, err error)

	// ReleaseHold undoes a hold on explicit cancellation or timeout.
	ReleaseHold(ctx context.Context, token string) error

	// CommitAllocation executes Layer 2 (Postgres) inside a single transaction with ORDER BY stay_date ASC locking.
	CommitAllocation(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error

	// GetDailyAllocations retrieves availability slices for search queries.
	GetDailyAllocations(ctx context.Context, propertyID, roomTypeID uuid.UUID, startDate, endDate time.Time) ([]*DailyAllocation, error)

	// BatchUpsertDailyAllocations inserts or updates rolling daily allocation records while preserving allocated counts.
	BatchUpsertDailyAllocations(ctx context.Context, allocations []*DailyAllocation) error
}

// FastHoldPort defines Layer 1 (Redis) fast-path inventory hold operations.
type FastHoldPort interface {
	AcquireFastHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (token string, expiresAt time.Time, err error)
	ReleaseFastHold(ctx context.Context, token string) error
}

// UserRepository manages user accounts and property assignments.
type UserRepository interface {
	CreateUser(ctx context.Context, user *identity.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*identity.User, error)
	GetByPhone(ctx context.Context, phone string) (*identity.User, error)
	GetByEmail(ctx context.Context, email string) (*identity.User, error)
	GetUserPropertyAssignments(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	AssignUserToProperty(ctx context.Context, userID, propertyID uuid.UUID) error
}

// PropertyRepository defines persistence operations for properties and room types.
type PropertyRepository interface {
	CreateProperty(ctx context.Context, p *property.Property) error
	GetPropertyByID(ctx context.Context, id uuid.UUID) (*property.Property, error)
	UpdateProperty(ctx context.Context, p *property.Property) error
	ListProperties(ctx context.Context, filter PropertyFilter) ([]*property.Property, error)
	
	CreateRoomType(ctx context.Context, rt *property.RoomType) error
	GetRoomTypeByID(ctx context.Context, id uuid.UUID) (*property.RoomType, error)
	ListRoomTypesByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.RoomType, error)
	
	CreateRoom(ctx context.Context, r *property.PhysicalRoom) error
	ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.PhysicalRoom, error)
}

// PaymentGateway is implemented once per provider (Telebirr, CBE Birr, Chapa).
type PaymentGateway interface {
	InitiateCharge(ctx context.Context, reservationRef string, amount types.Money, payerPhone string) (redirectURL string, err error)
	VerifyWebhookSignature(payload []byte, signature string) bool
}
