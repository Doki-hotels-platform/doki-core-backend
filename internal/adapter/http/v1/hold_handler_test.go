package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/adapter/http/v1/dto"
	"doki-backend/internal/domain"
	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/domain/property"
)

type mockFastHoldAdapter struct {
	acquireFn func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error)
	releaseFn func(ctx context.Context, token string) error
}

func (m *mockFastHoldAdapter) AcquireFastHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
	if m.acquireFn != nil {
		return m.acquireFn(ctx, propertyID, roomTypeID, checkIn, checkOut, totalCapacity, ttl)
	}
	return "hold:tok:test-abc-123", time.Now().UTC().Add(15 * time.Minute), nil
}

func (m *mockFastHoldAdapter) ReleaseFastHold(ctx context.Context, token string) error {
	if m.releaseFn != nil {
		return m.releaseFn(ctx, token)
	}
	return nil
}

type mockResRepo struct {
	createHoldFn func(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error)
}

func (m *mockResRepo) CreateHold(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
	if m.createHoldFn != nil {
		return m.createHoldFn(ctx, hold, guestName, guestPhone)
	}
	return uuid.New(), nil
}

func (m *mockResRepo) CreateReservation(ctx context.Context, res *domain.Reservation) error {
	return nil
}
func (m *mockResRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	return nil, nil
}
func (m *mockResRepo) GetByReference(ctx context.Context, ref string) (*domain.Reservation, error) {
	return nil, nil
}
func (m *mockResRepo) UpdateStatus(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error {
	return nil
}
func (m *mockResRepo) GetExpiredHolds(ctx context.Context, cutoff time.Time, limit int) ([]*domain.Reservation, error) {
	return nil, nil
}
func (m *mockResRepo) MarkReservationExpired(ctx context.Context, id uuid.UUID) error {
	return nil
}

type mockPropRepo struct {
	getRoomTypeFn func(ctx context.Context, id uuid.UUID) (*property.RoomType, error)
}

func (m *mockPropRepo) CreateProperty(ctx context.Context, p *property.Property) error { return nil }
func (m *mockPropRepo) UpdateProperty(ctx context.Context, p *property.Property) error { return nil }
func (m *mockPropRepo) GetPropertyByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	return nil, nil
}
func (m *mockPropRepo) ListProperties(ctx context.Context, filter property.PropertyFilter) ([]*property.Property, error) {
	return nil, nil
}
func (m *mockPropRepo) CreateRoomType(ctx context.Context, rt *property.RoomType) error { return nil }
func (m *mockPropRepo) GetRoomTypeByID(ctx context.Context, id uuid.UUID) (*property.RoomType, error) {
	if m.getRoomTypeFn != nil {
		return m.getRoomTypeFn(ctx, id)
	}
	return &property.RoomType{ID: id, TotalInventory: 10}, nil
}
func (m *mockPropRepo) ListRoomTypesByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.RoomType, error) {
	return nil, nil
}
func (m *mockPropRepo) CreateRoom(ctx context.Context, r *property.PhysicalRoom) error { return nil }
func (m *mockPropRepo) ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.PhysicalRoom, error) {
	return nil, nil
}

func TestHoldHandler_CreateHold_Success(t *testing.T) {
	mockFast := &mockFastHoldAdapter{}
	mockRes := &mockResRepo{}
	mockProp := &mockPropRepo{}

	holdService := inventory.NewHoldService(mockFast, mockRes)
	handler := NewHoldHandler(holdService, mockProp)

	reqPayload := dto.CreateHoldRequest{
		PropertyID: uuid.New().String(),
		RoomTypeID: uuid.New().String(),
		CheckIn:    time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02"),
		CheckOut:   time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02"),
		GuestName:  "Abebe Bikila",
		GuestPhone: "+251911223344",
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.CreateHold(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp dto.CreateHoldResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ReservationID == "" {
		t.Error("expected non-empty reservation_id")
	}
	if resp.HoldToken == "" {
		t.Error("expected non-empty hold_token")
	}
	if resp.Status != "INVENTORY_HOLD" {
		t.Errorf("expected status 'INVENTORY_HOLD', got '%s'", resp.Status)
	}
}

func TestHoldHandler_CreateHold_ExhaustedInventory_Returns409(t *testing.T) {
	mockFast := &mockFastHoldAdapter{
		acquireFn: func(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, totalCapacity int, ttl time.Duration) (string, time.Time, error) {
			return "", time.Time{}, domain.ErrInventoryUnavailable
		},
	}
	mockRes := &mockResRepo{}
	mockProp := &mockPropRepo{}

	holdService := inventory.NewHoldService(mockFast, mockRes)
	handler := NewHoldHandler(holdService, mockProp)

	reqPayload := dto.CreateHoldRequest{
		PropertyID: uuid.New().String(),
		RoomTypeID: uuid.New().String(),
		CheckIn:    time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02"),
		CheckOut:   time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02"),
		GuestName:  "Abebe Bikila",
		GuestPhone: "+251911223344",
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.CreateHold(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on sold out room, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHoldHandler_CreateHold_InvalidDateRange_Returns422(t *testing.T) {
	mockFast := &mockFastHoldAdapter{}
	mockRes := &mockResRepo{}
	mockProp := &mockPropRepo{}

	holdService := inventory.NewHoldService(mockFast, mockRes)
	handler := NewHoldHandler(holdService, mockProp)

	reqPayload := dto.CreateHoldRequest{
		PropertyID: uuid.New().String(),
		RoomTypeID: uuid.New().String(),
		CheckIn:    time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02"),
		CheckOut:   time.Now().UTC().Add(24 * time.Hour).Format("2006-01-02"), // checkOut before checkIn
		GuestName:  "Abebe Bikila",
		GuestPhone: "+251911223344",
	}

	body, _ := json.Marshal(reqPayload)
	req := httptest.NewRequest(http.MethodPost, "/v1/reservations/hold", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.CreateHold(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 Unprocessable Entity, got %d: %s", rec.Code, rec.Body.String())
	}
}
