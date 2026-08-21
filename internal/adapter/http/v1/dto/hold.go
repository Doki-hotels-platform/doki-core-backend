package dto

import "time"

// CreateHoldRequest represents the payload for POST /v1/reservations/hold.
type CreateHoldRequest struct {
	PropertyID string `json:"property_id"`
	RoomTypeID string `json:"room_type_id"`
	CheckIn    string `json:"check_in"` // YYYY-MM-DD
	CheckOut   string `json:"check_out"` // YYYY-MM-DD
	GuestName  string `json:"guest_name"`
	GuestPhone string `json:"guest_phone"`
}

// CreateHoldResponse represents the response returned on 201 Created.
type CreateHoldResponse struct {
	ReservationID string    `json:"reservation_id"`
	HoldToken     string    `json:"hold_token"`
	ExpiresAt     time.Time `json:"expires_at"`
	Status        string    `json:"status"`
}

// ErrorResponse defines standard API error payloads.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
