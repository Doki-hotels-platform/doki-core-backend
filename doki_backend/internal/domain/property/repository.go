package property

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrNotFound          = errors.New("property not found")
	ErrConflict          = errors.New("property resource conflict")
	ErrInvalidParameters = errors.New("invalid property parameters")
)

type PropertyFilter struct {
	Region   *string
	City     *string
	Category *Category
	Status   *Status
	Limit    int
	Offset   int
}

// Repository defines persistence operations for property aggregate.
type Repository interface {
	CreateProperty(ctx context.Context, p *Property) error
	GetPropertyByID(ctx context.Context, id uuid.UUID) (*Property, error)
	UpdateProperty(ctx context.Context, p *Property) error
	ListProperties(ctx context.Context, filter PropertyFilter) ([]*Property, error)

	CreateRoomType(ctx context.Context, rt *RoomType) error
	GetRoomTypeByID(ctx context.Context, id uuid.UUID) (*RoomType, error)
	ListRoomTypesByProperty(ctx context.Context, propertyID uuid.UUID) ([]*RoomType, error)

	CreateRoom(ctx context.Context, r *PhysicalRoom) error
	ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*PhysicalRoom, error)
}

// UserAssignmentRepository handles property staff assignments.
type UserAssignmentRepository interface {
	AssignUserToProperty(ctx context.Context, userID, propertyID uuid.UUID) error
}
