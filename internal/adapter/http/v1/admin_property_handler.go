package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"doki-backend/internal/adapter/http/v1/dto"
	"doki-backend/internal/domain"
	"doki-backend/internal/domain/property"
	"doki-backend/pkg/types"
)

type AdminPropertyHandler struct {
	propService *property.PropertyService
}

func NewAdminPropertyHandler(propService *property.PropertyService) *AdminPropertyHandler {
	return &AdminPropertyHandler{propService: propService}
}

// CreateProperty handles POST /v1/admin/properties.
func (h *AdminPropertyHandler) CreateProperty(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var reqBody dto.CreatePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	var ownerID *uuid.UUID
	if reqBody.OwnerUserID != nil && *reqBody.OwnerUserID != "" {
		if oid, err := uuid.Parse(*reqBody.OwnerUserID); err == nil {
			ownerID = &oid
		}
	}

	prop := &property.Property{
		ID:           uuid.New(),
		Code:         reqBody.Code,
		Name:         reqBody.Name,
		Category:     property.Category(reqBody.Category),
		Status:       property.StatusDraft,
		City:         reqBody.City,
		Region:       reqBody.Region,
		Latitude:     reqBody.Latitude,
		Longitude:    reqBody.Longitude,
		BaseCurrency: reqBody.BaseCurrency,
		OwnerUserID:  ownerID,
	}

	created, err := h.propService.CreateProperty(r.Context(), prop, ownerID)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "PROPERTY_CODE_EXISTS",
				Message: "A property with this code already exists",
			})
			return
		}

		if errors.Is(err, domain.ErrInvalidParameters) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_PARAMETERS",
				Message: "Code, name, city, and region are required fields",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to create property",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toPropertyResponse(created))
}

// GetProperty handles GET /v1/admin/properties/{property_id}.
func (h *AdminPropertyHandler) GetProperty(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	propIDStr := chi.URLParam(r, "property_id")
	propID, err := uuid.Parse(propIDStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PROPERTY_ID",
			Message: "property_id must be a valid UUID",
		})
		return
	}

	prop, err := h.propService.GetPropertyByID(r.Context(), propID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "PROPERTY_NOT_FOUND",
				Message: "Property not found",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to fetch property",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toPropertyResponse(prop))
}

// UpdateProperty handles PUT /v1/admin/properties/{property_id}.
func (h *AdminPropertyHandler) UpdateProperty(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	propIDStr := chi.URLParam(r, "property_id")
	propID, err := uuid.Parse(propIDStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PROPERTY_ID",
			Message: "property_id must be a valid UUID",
		})
		return
	}

	var reqBody dto.UpdatePropertyRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	propUpdate := &property.Property{
		ID:        propID,
		Name:      reqBody.Name,
		Category:  property.Category(reqBody.Category),
		Status:    property.Status(reqBody.Status),
		City:      reqBody.City,
		Region:    reqBody.Region,
		Latitude:  reqBody.Latitude,
		Longitude: reqBody.Longitude,
	}

	updated, err := h.propService.UpdateProperty(r.Context(), propUpdate)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "PROPERTY_NOT_FOUND",
				Message: "Property not found",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to update property",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toPropertyResponse(updated))
}

// CreateRoomType handles POST /v1/admin/properties/{property_id}/room-types.
func (h *AdminPropertyHandler) CreateRoomType(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	propIDStr := chi.URLParam(r, "property_id")
	propID, err := uuid.Parse(propIDStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PROPERTY_ID",
			Message: "property_id must be a valid UUID",
		})
		return
	}

	var reqBody dto.CreateRoomTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	rt := &property.RoomType{
		ID:             uuid.New(),
		PropertyID:     propID,
		Code:           reqBody.Code,
		Name:           reqBody.Name,
		Capacity:       reqBody.Capacity,
		BaseRateMinor:  types.NewMoney(reqBody.BaseRateMinor, "ETB"),
		TotalInventory: reqBody.TotalInventory,
	}

	created, err := h.propService.CreateRoomType(r.Context(), rt)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "ROOM_TYPE_CODE_EXISTS",
				Message: "A room type with this code already exists for this property",
			})
			return
		}

		if errors.Is(err, domain.ErrInvalidParameters) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_PARAMETERS",
				Message: "Code, name, capacity (>0), and total_inventory (>0) are required",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to create room type",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.RoomTypeResponse{
		ID:             created.ID.String(),
		PropertyID:     created.PropertyID.String(),
		Code:           created.Code,
		Name:           created.Name,
		Capacity:       created.Capacity,
		BaseRateMinor:  created.BaseRateMinor.AmountMinor,
		TotalInventory: created.TotalInventory,
		CreatedAt:      created.CreatedAt,
	})
}

// CreateRoom handles POST /v1/admin/properties/{property_id}/rooms.
func (h *AdminPropertyHandler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	propIDStr := chi.URLParam(r, "property_id")
	propID, err := uuid.Parse(propIDStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INVALID_PROPERTY_ID",
			Message: "property_id must be a valid UUID",
		})
		return
	}

	var reqBody dto.CreateRoomRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
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

	rm := &property.PhysicalRoom{
		ID:            uuid.New(),
		PropertyID:    propID,
		RoomTypeID:    roomTypeID,
		RoomNumber:    reqBody.RoomNumber,
		Floor:         reqBody.Floor,
		IsOperational: true,
		IsClean:       true,
	}

	created, err := h.propService.CreateRoom(r.Context(), rm)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "ROOM_NUMBER_EXISTS",
				Message: "A room with this room number already exists for this property",
			})
			return
		}

		if errors.Is(err, domain.ErrInvalidParameters) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_PARAMETERS",
				Message: "room_type_id and room_number are required",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to create physical room",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.RoomResponse{
		ID:            created.ID.String(),
		PropertyID:    created.PropertyID.String(),
		RoomTypeID:    created.RoomTypeID.String(),
		RoomNumber:    created.RoomNumber,
		Floor:         created.Floor,
		IsOperational: created.IsOperational,
		IsClean:       created.IsClean,
	})
}

func toPropertyResponse(p *property.Property) dto.PropertyResponse {
	var ownerIDStr *string
	if p.OwnerUserID != nil {
		s := p.OwnerUserID.String()
		ownerIDStr = &s
	}
	return dto.PropertyResponse{
		ID:           p.ID.String(),
		Code:         p.Code,
		Name:         p.Name,
		Category:     string(p.Category),
		Status:       string(p.Status),
		City:         p.City,
		Region:       p.Region,
		Latitude:     p.Latitude,
		Longitude:    p.Longitude,
		BaseCurrency: p.BaseCurrency,
		OwnerUserID:  ownerIDStr,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}
}
