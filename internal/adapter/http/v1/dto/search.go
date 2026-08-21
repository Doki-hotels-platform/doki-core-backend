package dto

import "time"

// SearchPropertiesRequest contains query criteria for property availability search.
type SearchPropertiesRequest struct {
	Region   *string   `json:"region,omitempty"`
	City     *string   `json:"city,omitempty"`
	CheckIn  time.Time `json:"check_in"`
	CheckOut time.Time `json:"check_out"`
	Guests   int       `json:"guests"`
	Page     int       `json:"page"`
	PageSize int       `json:"page_size"`
}

// AvailableRoomType represents an available room category with calculated nightly rates.
type AvailableRoomType struct {
	RoomTypeID       string `json:"room_type_id"`
	Name             string `json:"name"`
	Capacity         int    `json:"capacity"`
	NightlyRateMinor int64  `json:"nightly_rate_minor"`
	Currency         string `json:"currency"`
	UnitsAvailable   int    `json:"units_available"`
}

// PropertySearchResult encapsulates a property with matching available room types.
type PropertySearchResult struct {
	PropertyID         string              `json:"property_id"`
	Code               string              `json:"code"`
	Name               string              `json:"name"`
	Category           string              `json:"category"`
	City               string              `json:"city"`
	Region             string              `json:"region"`
	Latitude           *float64            `json:"latitude,omitempty"`
	Longitude          *float64            `json:"longitude,omitempty"`
	AvailableRoomTypes []AvailableRoomType `json:"available_room_types"`
}

// SearchPropertiesResponse represents the paginated response for GET /v1/properties/search.
type SearchPropertiesResponse struct {
	Results      []PropertySearchResult `json:"results"`
	Page         int                    `json:"page"`
	PageSize     int                    `json:"page_size"`
	TotalResults int                    `json:"total_results"`
}
