package v1

import (
	"encoding/json"
	"errors"
	"net/http"

	"doki-backend/internal/adapter/http/v1/dto"
	"doki-backend/internal/domain"
	"doki-backend/internal/domain/identity"
)

type AuthHandler struct {
	authService *identity.AuthService
}

func NewAuthHandler(authService *identity.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// Register handles POST /v1/auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var reqBody dto.RegisterUserRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	role := identity.RoleCustomer
	if reqBody.Role != "" {
		role = identity.Role(reqBody.Role)
	}

	user, err := h.authService.RegisterUser(r.Context(), identity.RegisterRequest{
		PhoneNumber: reqBody.PhoneNumber,
		Email:       reqBody.Email,
		Password:    reqBody.Password,
		FullName:    reqBody.FullName,
		Role:        role,
		Region:      reqBody.Region,
	})

	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "USER_ALREADY_EXISTS",
				Message: "A user with this phone number or email already exists",
			})
			return
		}

		if errors.Is(err, domain.ErrPasswordTooShort) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "PASSWORD_TOO_SHORT",
				Message: "Password must be at least 8 characters long",
			})
			return
		}

		if errors.Is(err, domain.ErrInvalidParameters) {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_PARAMETERS",
				Message: "Phone number and full name are required",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Failed to register user",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(dto.UserDTO{
		ID:          user.ID.String(),
		PhoneNumber: user.PhoneNumber,
		Email:       user.Email,
		FullName:    user.FullName,
		Role:        user.Role.String(),
		Region:      user.Region,
	})
}

// Login handles POST /v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var reqBody dto.LoginUserRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "MALFORMED_JSON",
			Message: "Invalid JSON request payload",
		})
		return
	}

	token, user, err := h.authService.Login(r.Context(), reqBody.Identifier, reqBody.Password)
	if err != nil {
		if errors.Is(err, domain.ErrUnauthorized) {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
				Error:   "INVALID_CREDENTIALS",
				Message: "Invalid phone/email or password",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(dto.ErrorResponse{
			Error:   "INTERNAL_SERVER_ERROR",
			Message: "Authentication failed",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(dto.AuthResponse{
		Token: token,
		User: dto.UserDTO{
			ID:          user.ID.String(),
			PhoneNumber: user.PhoneNumber,
			Email:       user.Email,
			FullName:    user.FullName,
			Role:        user.Role.String(),
			Region:      user.Region,
		},
	})
}
