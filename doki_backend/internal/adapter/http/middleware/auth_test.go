package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"doki-backend/internal/domain/identity"
)

func TestAuthAndRBACMiddleware(t *testing.T) {
	secret := []byte("test-secret-key-1234567890123456")
	userID := uuid.New()
	prop1 := uuid.New()
	prop2 := uuid.New()

	// Generate token for Hotel Manager assigned to prop1
	token, err := GenerateToken(userID, identity.RoleHotelManager, nil, []uuid.UUID{prop1}, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Router setup
	r := chi.NewRouter()
	r.Use(AuthMiddleware(secret))

	var reached bool
	r.With(RequireRoles(identity.RoleHotelManager), RequirePropertyScope("property_id")).
		Get("/properties/{property_id}/dashboard", func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusOK)
		})

	// 1. Valid request with authorized property
	req := httptest.NewRequest(http.MethodGet, "/properties/"+prop1.String()+"/dashboard", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rr.Code)
	}
	if !reached {
		t.Error("expected handler to be reached")
	}

	// 2. Request for unassigned property -> should be 403 Forbidden
	reached = false
	reqUnauthorizedProp := httptest.NewRequest(http.MethodGet, "/properties/"+prop2.String()+"/dashboard", nil)
	reqUnauthorizedProp.Header.Set("Authorization", "Bearer "+token)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, reqUnauthorizedProp)

	if rr2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", rr2.Code)
	}
	if reached {
		t.Error("expected handler NOT to be reached for unauthorized property")
	}

	// 3. Request without token -> should be 401 Unauthorized
	reqNoAuth := httptest.NewRequest(http.MethodGet, "/properties/"+prop1.String()+"/dashboard", nil)
	rr3 := httptest.NewRecorder()
	r.ServeHTTP(rr3, reqNoAuth)

	if rr3.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rr3.Code)
	}
}
