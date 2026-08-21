package identity

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	BcryptCost = 12
)

// AuthService handles authentication, password hashing, and token issuance delegation.
type AuthService struct {
	userRepo    UserRepository
	tokenIssuer TokenIssuer
}

func NewAuthService(userRepo UserRepository, tokenIssuer TokenIssuer) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		tokenIssuer: tokenIssuer,
	}
}

type RegisterRequest struct {
	PhoneNumber string
	Email       *string
	Password    string
	FullName    string
	Role        Role
	Region      *string
}

func (s *AuthService) RegisterUser(ctx context.Context, req RegisterRequest) (*User, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	req.PhoneNumber = strings.TrimSpace(req.PhoneNumber)
	if req.PhoneNumber == "" {
		return nil, ErrInvalidParameters
	}

	if len(req.Password) < 8 {
		return nil, ErrPasswordTooShort
	}

	if req.FullName == "" {
		return nil, ErrInvalidParameters
	}

	if req.Role == "" {
		req.Role = RoleCustomer
	}

	// Bcrypt password hashing (Cost Factor 12)
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(req.Password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &User{
		ID:           uuid.New(),
		PhoneNumber:  req.PhoneNumber,
		Email:        req.Email,
		PasswordHash: string(hashedBytes),
		FullName:     req.FullName,
		Role:         req.Role,
		Region:       req.Region,
		IsActive:     true,
	}

	if err := s.userRepo.CreateUser(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

func (s *AuthService) Login(ctx context.Context, identifier, password string) (string, *User, error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	identifier = strings.TrimSpace(identifier)
	if identifier == "" || password == "" {
		return "", nil, ErrUnauthorized
	}

	var user *User
	var err error

	// Detect if identifier is email or phone number
	if strings.Contains(identifier, "@") {
		user, err = s.userRepo.GetByEmail(ctx, identifier)
	} else {
		user, err = s.userRepo.GetByPhone(ctx, identifier)
	}

	if err != nil || !user.IsActive {
		return "", nil, ErrUnauthorized
	}

	// Verify bcrypt hash
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return "", nil, ErrUnauthorized
	}

	// Retrieve assigned properties for staff/owners
	propertyIDs, err := s.userRepo.GetUserPropertyAssignments(ctx, user.ID)
	if err != nil {
		return "", nil, fmt.Errorf("fetch user property assignments: %w", err)
	}

	if s.tokenIssuer == nil {
		return "", nil, fmt.Errorf("token issuer not configured")
	}

	// Delegate token issuance to TokenIssuer port adapter
	token, err := s.tokenIssuer.GenerateToken(user, propertyIDs)
	if err != nil {
		return "", nil, fmt.Errorf("generate auth token: %w", err)
	}

	return token, user, nil
}
