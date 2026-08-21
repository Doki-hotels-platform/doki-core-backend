package domain

import (
	"time"

	"github.com/google/uuid"
)

// InventoryHold represents a short-lived lock on room availability.
type InventoryHold struct {
	Token      string    `json:"token"`
	PropertyID uuid.UUID `json:"property_id"`
	RoomTypeID uuid.UUID `json:"room_type_id"`
	CheckIn    time.Time `json:"check_in"`
	CheckOut   time.Time `json:"check_out"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Reservation represents the booking aggregate.
type Reservation struct {
	ID               uuid.UUID  `json:"id"`
	BookingReference string     `json:"booking_reference"`
	PropertyID       uuid.UUID  `json:"property_id"`
	RoomTypeID       uuid.UUID  `json:"room_type_id"`
	GuestName        string     `json:"guest_name"`
	GuestPhone       string     `json:"guest_phone"`
	CheckInDate      time.Time  `json:"check_in_date"`
	CheckOutDate     time.Time  `json:"check_out_date"`
	Status           string     `json:"status"`
	HoldExpiresAt    *time.Time `json:"hold_expires_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
