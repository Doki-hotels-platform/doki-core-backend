package v1

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/adapter/http/v1/dto"
	"doki-backend/internal/domain"
	"doki-backend/internal/domain/inventory"
)

type HoldHandler struct {
	holdService  *inventory.HoldService
	propertyRepo domain.PropertyRepository
}

func NewHoldHandler(holdService *inventory.HoldService, propertyRepo domain.PropertyRepository) *HoldHandler {
	return &HoldHandler{
		holdService:  holdService,
		propertyRepo: propertyRepo,
	}
}

// CreateHold handles POST /v1/reservations/hold.
func (h *HoldHandler) CreateHold(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var reqBody dto.CreateHoldRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	// 1. Validate UUIDs
	propID, err := uuid.Parse(reqBody.PropertyID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PROPERTY_ID",
			Message: "property_id must be a valid UUID",
		})
		return
	}

	roomTypeID, err := uuid.Parse(reqBody.RoomTypeID)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_ROOM_TYPE_ID",
			Message: "room_type_id must be a valid UUID",
		})
		return
	}

	// 2. Validate Dates
	checkIn, err := time.Parse("2006-01-02", reqBody.CheckIn)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_CHECK_IN",
			Message: "check_in must be formatted as YYYY-MM-DD",
		})
		return
	}

	checkOut, err := time.Parse("2006-01-02", reqBody.CheckOut)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_CHECK_OUT",
			Message: "check_out must be formatted as YYYY-MM-DD",
		})
		return
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !checkOut.After(checkIn) || checkIn.Before(today) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_DATE_RANGE",
			Message: "check_out must be after check_in and dates cannot be in the past",
		})
		return
	}

	// 3. Validate Guest Details
	if reqBody.GuestName == "" || len(reqBody.GuestName) > 150 {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_GUEST_NAME",
			Message: "guest_name is required (max 150 characters)",
		})
		return
	}

	if reqBody.GuestPhone == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_GUEST_PHONE",
			Message: "guest_phone is required",
		})
		return
	}

	// 4. Fetch Room Type Capacity
	capacity := 100 // fallback default
	if h.propertyRepo != nil {
		rt, err := h.propertyRepo.GetRoomTypeByID(r.Context(), roomTypeID)
		if err == nil && rt != nil && rt.TotalInventory > 0 {
			capacity = rt.TotalInventory
		}
	}

	// 5. Invoke Two-Tier Hold Service
	holdReq := inventory.CreateHoldRequest{
		PropertyID:    propID,
		RoomTypeID:    roomTypeID,
		CheckIn:       checkIn,
		CheckOut:      checkOut,
		TotalCapacity: capacity,
		GuestName:     reqBody.GuestName,
		GuestPhone:    reqBody.GuestPhone,
	}

	result, err := h.holdService.CreateHold(r.Context(), holdReq)
	if err != nil {
		if errors.Is(err, domain.ErrInventoryUnavailable) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVENTORY_UNAVAILABLE",
				Message: "Requested room type is sold out for one or more dates in the range",
			})
			return
		}

		if errors.Is(err, domain.ErrInvalidDateRange) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_DATE_RANGE",
				Message: "Check-out must be after check-in and dates cannot be in the past",
			})
			return
		}

		if errors.Is(err, domain.ErrConflict) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "CONFLICT",
				Message: "Resource conflict occurred during hold reservation",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to acquire inventory hold",
		})
		return
	}

	// 6. Return 201 Created
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.CreateHoldResponse{
		ReservationID: result.ReservationID.String(),
		HoldToken:     result.HoldToken,
		ExpiresAt:     result.ExpiresAt,
		Status:        result.Status,
	})
}
