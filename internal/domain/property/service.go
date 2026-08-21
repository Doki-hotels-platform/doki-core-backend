package property

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// PropertyService orchestrates property lifecycle, room type provisioning, and physical rooms.
type PropertyService struct {
	propRepo Repository
	userRepo UserAssignmentRepository
}

func NewPropertyService(propRepo Repository, userRepo UserAssignmentRepository) *PropertyService {
	return &PropertyService{
		propRepo: propRepo,
		userRepo: userRepo,
	}
}

// CreateProperty initializes a new property aggregate and assigns owner scope.
func (s *PropertyService) CreateProperty(ctx context.Context, p *Property, ownerID *uuid.UUID) (*Property, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	p.Code = strings.ToUpper(strings.TrimSpace(p.Code))
	p.Name = strings.TrimSpace(p.Name)
	p.City = strings.TrimSpace(p.City)
	p.Region = strings.TrimSpace(p.Region)

	if p.Code == "" || p.Name == "" || p.City == "" || p.Region == "" {
		return nil, ErrInvalidParameters
	}

	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}

	if p.Status == "" {
		p.Status = StatusDraft
	}

	if p.Category == "" {
		p.Category = CategoryBranded
	}

	if ownerID != nil && *ownerID != uuid.Nil {
		p.OwnerUserID = ownerID
	}

	if err := s.propRepo.CreateProperty(ctx, p); err != nil {
		return nil, fmt.Errorf("create property: %w", err)
	}

	// Explicitly assign property ownership in user_property_assignment
	if p.OwnerUserID != nil && s.userRepo != nil {
		_ = s.userRepo.AssignUserToProperty(ctx, *p.OwnerUserID, p.ID)
	}

	return p, nil
}

// GetPropertyByID retrieves property details.
func (s *PropertyService) GetPropertyByID(ctx context.Context, id uuid.UUID) (*Property, error) {
	return s.propRepo.GetPropertyByID(ctx, id)
}

// UpdateProperty modifies property metadata and manages status transitions.
func (s *PropertyService) UpdateProperty(ctx context.Context, p *Property) (*Property, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	existing, err := s.propRepo.GetPropertyByID(ctx, p.ID)
	if err != nil {
		return nil, err
	}

	if p.Name != "" {
		existing.Name = strings.TrimSpace(p.Name)
	}
	if p.Category != "" {
		existing.Category = p.Category
	}
	if p.Status != "" {
		existing.Status = p.Status
	}
	if p.City != "" {
		existing.City = strings.TrimSpace(p.City)
	}
	if p.Region != "" {
		existing.Region = strings.TrimSpace(p.Region)
	}
	if p.Latitude != nil {
		existing.Latitude = p.Latitude
	}
	if p.Longitude != nil {
		existing.Longitude = p.Longitude
	}

	if err := s.propRepo.UpdateProperty(ctx, existing); err != nil {
		return nil, fmt.Errorf("update property: %w", err)
	}

	return existing, nil
}

// CreateRoomType provisions a new room type category under a property.
func (s *PropertyService) CreateRoomType(ctx context.Context, rt *RoomType) (*RoomType, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rt.Code = strings.ToUpper(strings.TrimSpace(rt.Code))
	rt.Name = strings.TrimSpace(rt.Name)

	if rt.PropertyID == uuid.Nil || rt.Code == "" || rt.Name == "" || rt.Capacity <= 0 || rt.TotalInventory <= 0 {
		return nil, ErrInvalidParameters
	}

	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}

	if err := s.propRepo.CreateRoomType(ctx, rt); err != nil {
		return nil, fmt.Errorf("create room type: %w", err)
	}

	return rt, nil
}

// CreateRoom provisions an individual physical room unit.
func (s *PropertyService) CreateRoom(ctx context.Context, rm *PhysicalRoom) (*PhysicalRoom, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rm.RoomNumber = strings.TrimSpace(rm.RoomNumber)
	if rm.PropertyID == uuid.Nil || rm.RoomTypeID == uuid.Nil || rm.RoomNumber == "" {
		return nil, ErrInvalidParameters
	}

	if rm.ID == uuid.Nil {
		rm.ID = uuid.New()
	}

	if err := s.propRepo.CreateRoom(ctx, rm); err != nil {
		return nil, fmt.Errorf("create physical room: %w", err)
	}

	return rm, nil
}

// ListRoomsByProperty returns all physical rooms belonging to a property.
func (s *PropertyService) ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*PhysicalRoom, error) {
	return s.propRepo.ListRoomsByProperty(ctx, propertyID)
}
