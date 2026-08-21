package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

var (
	ErrUnauthorized      = errors.New("unauthorized: missing or invalid credentials")
	ErrForbidden         = errors.New("forbidden: insufficient permissions")
	ErrUserNotFound      = errors.New("user not found")
	ErrConflict          = errors.New("user with this phone or email already exists")
	ErrInvalidParameters = errors.New("invalid user registration parameters")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters long")
)

// UserRepository manages user accounts and property assignments.
type UserRepository interface {
	CreateUser(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByPhone(ctx context.Context, phone string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetUserPropertyAssignments(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	AssignUserToProperty(ctx context.Context, userID, propertyID uuid.UUID) error
}
