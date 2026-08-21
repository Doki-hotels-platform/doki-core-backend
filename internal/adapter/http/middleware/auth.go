package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"doki-backend/internal/domain/identity"
	"doki-backend/internal/platform/logger"
)

type userContextKey string

const (
	UserKey userContextKey = "authenticated_user"
)

// UserClaims defines the JWT payload structure.
type UserClaims struct {
	UserID             uuid.UUID     `json:"sub"`
	Role               identity.Role `json:"role"`
	Region             *string       `json:"region,omitempty"`
	PropertyAssignment []uuid.UUID   `json:"property_ids,omitempty"`
	jwt.RegisteredClaims
}

// AuthenticatedUser holds user details stored in request context.
type AuthenticatedUser struct {
	ID                 uuid.UUID
	Role               identity.Role
	Region             *string
	PropertyAssignment map[uuid.UUID]bool
}

// AuthMiddleware validates HS256 signed JWT tokens and injects AuthenticatedUser into context.
func AuthMiddleware(secretKey []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"missing or malformed authorization header"}`, http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims := &UserClaims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing algorithm")
				}
				return secretKey, nil
			})

			if err != nil || !token.Valid {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			propMap := make(map[uuid.UUID]bool, len(claims.PropertyAssignment))
			for _, pid := range claims.PropertyAssignment {
				propMap[pid] = true
			}

			authUser := &AuthenticatedUser{
				ID:                 claims.UserID,
				Role:               claims.Role,
				Region:             claims.Region,
				PropertyAssignment: propMap,
			}

			ctx := context.WithValue(r.Context(), UserKey, authUser)
			ctx = logger.WithActorUserID(ctx, authUser.ID.String())

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetAuthenticatedUser extracts the AuthenticatedUser from request context.
func GetAuthenticatedUser(ctx context.Context) (*AuthenticatedUser, bool) {
	u, ok := ctx.Value(UserKey).(*AuthenticatedUser)
	return u, ok
}

// GenerateToken helper for test fixtures and login handlers.
func GenerateToken(userID uuid.UUID, role identity.Role, region *string, propertyIDs []uuid.UUID, secretKey []byte, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := UserClaims{
		UserID:             userID,
		Role:               role,
		Region:             region,
		PropertyAssignment: propertyIDs,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "doki-auth-engine",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secretKey)
}
