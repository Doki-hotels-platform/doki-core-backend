package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"doki-backend/internal/domain/identity"
)

// RequireRoles restricts endpoint access to specified user roles.
func RequireRoles(allowedRoles ...identity.Role) func(http.Handler) http.Handler {
	roleMap := make(map[identity.Role]bool, len(allowedRoles))
	for _, r := range allowedRoles {
		roleMap[r] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetAuthenticatedUser(r.Context())
			if !ok {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			// HQ_ADMIN always possesses global super-admin clearance
			if user.Role == identity.RoleHQAdmin {
				next.ServeHTTP(w, r)
				return
			}

			if !roleMap[user.Role] {
				http.Error(w, `{"error":"FORBIDDEN","message":"insufficient role permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequirePropertyScope ensures staff members only access properties assigned to their account.
func RequirePropertyScope(paramName string) func(http.Handler) http.Handler {
	if paramName == "" {
		paramName = "property_id"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetAuthenticatedUser(r.Context())
			if !ok {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			// HQ_ADMIN has global property clearance
			if user.Role == identity.RoleHQAdmin {
				next.ServeHTTP(w, r)
				return
			}

			propIDStr := chi.URLParam(r, paramName)
			if propIDStr == "" {
				propIDStr = r.URL.Query().Get(paramName)
			}

			if propIDStr == "" {
				http.Error(w, `{"error":"BAD_REQUEST","message":"missing property_id identifier"}`, http.StatusBadRequest)
				return
			}

			targetPropID, err := uuid.Parse(propIDStr)
			if err != nil {
				http.Error(w, `{"error":"BAD_REQUEST","message":"invalid property_id format"}`, http.StatusBadRequest)
				return
			}

			// Regional supervisors can access properties in their assigned region (verified at service layer or query)
			if user.Role == identity.RoleRegionalSupervisor {
				next.ServeHTTP(w, r)
				return
			}

			// Property-scoped staff (Owner, Manager, Receptionist) must have explicit assignment
			if !user.PropertyAssignment[targetPropID] {
				http.Error(w, `{"error":"FORBIDDEN","message":"access denied: user not assigned to this property"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireRegionalScope ensures REGIONAL_SUPERVISOR can only access operations within their assigned region.
func RequireRegionalScope(regionParamName string) func(http.Handler) http.Handler {
	if regionParamName == "" {
		regionParamName = "region"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := GetAuthenticatedUser(r.Context())
			if !ok {
				http.Error(w, `{"error":"UNAUTHORIZED","message":"authentication required"}`, http.StatusUnauthorized)
				return
			}

			if user.Role == identity.RoleHQAdmin {
				next.ServeHTTP(w, r)
				return
			}

			if user.Role == identity.RoleRegionalSupervisor {
				targetRegion := chi.URLParam(r, regionParamName)
				if targetRegion == "" {
					targetRegion = r.URL.Query().Get(regionParamName)
				}

				if targetRegion != "" && (user.Region == nil || *user.Region != targetRegion) {
					http.Error(w, `{"error":"FORBIDDEN","message":"access denied: outside supervisor assigned region"}`, http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
