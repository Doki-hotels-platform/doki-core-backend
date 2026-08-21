package identity

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	BcryptCost    = 12
	DefaultJWTTTL = 24 * time.Hour
)

// AuthService handles authentication, password hashing, and token issuance.
type AuthService struct {
	userRepo  UserRepository
	jwtSecret []byte
	tokenTTL  time.Duration
}

func NewAuthService(userRepo UserRepository, jwtSecret []byte, tokenTTL time.Duration) *AuthService {
	if tokenTTL <= 0 {
		tokenTTL = DefaultJWTTTL
	}
	return &AuthService{
		userRepo:  userRepo,
		jwtSecret: jwtSecret,
		tokenTTL:  tokenTTL,
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

	if err != nil {
		return "", nil, ErrUnauthorized
	}

	if !user.IsActive {
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

	// Issue HS256 signed JWT
	token, err := s.generateToken(user, propertyIDs)
	if err != nil {
		return "", nil, fmt.Errorf("generate auth token: %w", err)
	}

	return token, user, nil
}

type jwtCustomClaims struct {
	UserID             uuid.UUID   `json:"sub"`
	Role               Role        `json:"role"`
	Region             *string     `json:"region,omitempty"`
	PropertyAssignment []uuid.UUID `json:"property_ids,omitempty"`
	jwt.RegisteredClaims
}

func (s *AuthService) generateToken(user *User, propertyIDs []uuid.UUID) (string, error) {
	now := time.Now().UTC()
	claims := jwtCustomClaims{
		UserID:             user.ID,
		Role:               user.Role,
		Region:             user.Region,
		PropertyAssignment: propertyIDs,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.tokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "doki-auth-engine",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}
