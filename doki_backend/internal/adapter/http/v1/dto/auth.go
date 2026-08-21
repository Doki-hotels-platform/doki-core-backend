package dto

// RegisterUserRequest represents the payload for POST /v1/auth/register.
type RegisterUserRequest struct {
	PhoneNumber string  `json:"phone_number"`
	Email       *string `json:"email,omitempty"`
	Password    string  `json:"password"`
	FullName    string  `json:"full_name"`
	Role        string  `json:"role,omitempty"`
	Region      *string `json:"region,omitempty"`
}

// LoginUserRequest represents the payload for POST /v1/auth/login.
type LoginUserRequest struct {
	Identifier string `json:"identifier"` // phone_number or email
	Password   string `json:"password"`
}

// UserDTO represents user profile information returned in API responses.
type UserDTO struct {
	ID          string  `json:"id"`
	PhoneNumber string  `json:"phone_number"`
	Email       *string `json:"email,omitempty"`
	FullName    string  `json:"full_name"`
	Role        string  `json:"role"`
	Region      *string `json:"region,omitempty"`
}

// AuthResponse represents the response containing the issued JWT token.
type AuthResponse struct {
	Token string  `json:"token"`
	User  UserDTO `json:"user"`
}
