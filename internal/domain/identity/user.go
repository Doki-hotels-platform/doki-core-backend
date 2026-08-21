package identity

import (
	"time"

	"github.com/google/uuid"
)

// Role defines the authorization level of an application user.
type Role string

const (
	RoleHQAdmin            Role = "HQ_ADMIN"
	RoleRegionalSupervisor Role = "REGIONAL_SUPERVISOR"
	RoleHotelOwner         Role = "HOTEL_OWNER"
	RoleHotelManager       Role = "HOTEL_MANAGER"
	RoleReceptionist       Role = "RECEPTIONIST"
	RoleCustomer           Role = "CUSTOMER"
	RoleCorporate          Role = "CORPORATE"
)

func (r Role) String() string {
	return string(r)
}

func (r Role) IsStaff() bool {
	switch r {
	case RoleHQAdmin, RoleRegionalSupervisor, RoleHotelOwner, RoleHotelManager, RoleReceptionist:
		return true
	default:
		return false
	}
}

// User represents an authenticated identity in the DOKI platform.
type User struct {
	ID           uuid.UUID `json:"id"`
	PhoneNumber  string    `json:"phone_number"`
	Email        *string   `json:"email,omitempty"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	Role         Role      `json:"role"`
	Region       *string   `json:"region,omitempty"` // Populated for RegionalSupervisor
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
