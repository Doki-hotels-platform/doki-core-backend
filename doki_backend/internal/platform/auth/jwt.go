package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"doki-backend/internal/domain/identity"
)

// JWTTokenIssuer implements identity.TokenIssuer using HS256 JWT tokens.
type JWTTokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

// NewJWTTokenIssuer creates a new JWT token issuer.
func NewJWTTokenIssuer(secret []byte, ttl time.Duration) *JWTTokenIssuer {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &JWTTokenIssuer{
		secret: secret,
		ttl:    ttl,
	}
}

type jwtCustomClaims struct {
	UserID             uuid.UUID     `json:"sub"`
	Role               identity.Role `json:"role"`
	Region             *string       `json:"region,omitempty"`
	PropertyAssignment []uuid.UUID   `json:"property_ids,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken generates and signs an HS256 JWT for the given user.
func (j *JWTTokenIssuer) GenerateToken(user *identity.User, propertyIDs []uuid.UUID) (string, error) {
	now := time.Now().UTC()
	claims := jwtCustomClaims{
		UserID:             user.ID,
		Role:               user.Role,
		Region:             user.Region,
		PropertyAssignment: propertyIDs,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "doki-auth-engine",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}
