package dto

import "time"

type CreatePropertyRequest struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	City         string   `json:"city"`
	Region       string   `json:"region"`
	Latitude     *float64 `json:"latitude,omitempty"`
	Longitude    *float64 `json:"longitude,omitempty"`
	BaseCurrency string   `json:"base_currency,omitempty"`
	OwnerUserID  *string  `json:"owner_user_id,omitempty"`
}

type UpdatePropertyRequest struct {
	Name      string   `json:"name,omitempty"`
	Category  string   `json:"category,omitempty"`
	Status    string   `json:"status,omitempty"`
	City      string   `json:"city,omitempty"`
	Region    string   `json:"region,omitempty"`
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
}

type PropertyResponse struct {
	ID           string    `json:"id"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Category     string    `json:"category"`
	Status       string    `json:"status"`
	City         string    `json:"city"`
	Region       string    `json:"region"`
	Latitude     *float64  `json:"latitude,omitempty"`
	Longitude    *float64  `json:"longitude,omitempty"`
	BaseCurrency string    `json:"base_currency"`
	OwnerUserID  *string   `json:"owner_user_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type CreateRoomTypeRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Capacity       int16  `json:"capacity"`
	BaseRateMinor  int64  `json:"base_rate_minor"`
	TotalInventory int    `json:"total_inventory"`
}

type RoomTypeResponse struct {
	ID             string    `json:"id"`
	PropertyID     string    `json:"property_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Capacity       int16     `json:"capacity"`
	BaseRateMinor  int64     `json:"base_rate_minor"`
	TotalInventory int       `json:"total_inventory"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateRoomRequest struct {
	RoomTypeID string  `json:"room_type_id"`
	RoomNumber string  `json:"room_number"`
	Floor      *string `json:"floor,omitempty"`
}

type RoomResponse struct {
	ID            string  `json:"id"`
	PropertyID    string  `json:"property_id"`
	RoomTypeID    string  `json:"room_type_id"`
	RoomNumber    string  `json:"room_number"`
	Floor         *string `json:"floor,omitempty"`
	IsOperational bool    `json:"is_operational"`
	IsClean       bool    `json:"is_clean"`
}
