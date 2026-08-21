package property

import (
	"time"

	"github.com/google/uuid"

	"doki-backend/pkg/types"
)

type Category string

const (
	CategoryBranded   Category = "BRANDED"
	CategoryAffiliate Category = "AFFILIATE"
	CategoryOverflow  Category = "OVERFLOW"
)

type Status string

const (
	StatusDraft               Status = "DRAFT"
	StatusPendingVerification Status = "PENDING_VERIFICATION"
	StatusActive              Status = "ACTIVE"
	StatusSuspended           Status = "SUSPENDED"
	StatusDeactivated         Status = "DEACTIVATED"
)

// Property represents a hotel or serviced residence property.
type Property struct {
	ID           uuid.UUID  `json:"id"`
	Code         string     `json:"code"`
	Name         string     `json:"name"`
	Category     Category   `json:"category"`
	Status       Status     `json:"status"`
	Region       string     `json:"region"`
	City         string     `json:"city"`
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
	BaseCurrency string     `json:"base_currency"`
	CheckInTime  string     `json:"checkin_time"`
	CheckOutTime string     `json:"checkout_time"`
	OwnerUserID  *uuid.UUID `json:"owner_user_id,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// RoomType defines a category of rooms with specific capacity and pricing.
type RoomType struct {
	ID             uuid.UUID   `json:"id"`
	PropertyID     uuid.UUID   `json:"property_id"`
	Code           string      `json:"code"`
	Name           string      `json:"name"`
	Capacity       int16       `json:"capacity"`
	BaseRateMinor  types.Money `json:"base_rate_minor"`
	TotalInventory int         `json:"total_inventory"`
	CreatedAt      time.Time   `json:"created_at"`
}

// PhysicalRoom represents a physical room instance assigned to a stay.
type PhysicalRoom struct {
	ID            uuid.UUID `json:"id"`
	PropertyID    uuid.UUID `json:"property_id"`
	RoomTypeID    uuid.UUID `json:"room_type_id"`
	RoomNumber    string    `json:"room_number"`
	Floor         *string   `json:"floor,omitempty"`
	IsOperational bool      `json:"is_operational"`
	IsClean       bool      `json:"is_clean"`
}
