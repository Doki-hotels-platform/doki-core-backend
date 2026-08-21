package domain

import "errors"

// Shared domain sentinel errors across all bounded contexts.
// Pure business logic errors with zero framework dependencies.
var (
	// Inventory & Reservation Errors
	ErrInventoryUnavailable = errors.New("insufficient inventory for selected dates")
	ErrHoldExpired          = errors.New("reservation hold has expired")
	ErrHoldNotFound         = errors.New("hold token not found or already released")
	ErrInvalidDateRange     = errors.New("invalid date range: check-out must be after check-in")
	ErrReservationNotFound  = errors.New("reservation not found")
	ErrInvalidStatusChange  = errors.New("invalid reservation state transition")

	// Authentication & Authorization Errors
	ErrUnauthorized         = errors.New("unauthorized: missing or invalid credentials")
	ErrForbidden            = errors.New("forbidden: insufficient permissions for this operation")
	ErrUserNotFound         = errors.New("user not found")
	ErrUserInactive         = errors.New("user account is inactive")

	// Resource & Entity Errors
	ErrNotFound             = errors.New("requested resource not found")
	ErrConflict             = errors.New("resource conflict occurred")
	ErrAlreadyExists        = errors.New("resource already exists")
	ErrValidation           = errors.New("validation failed: invalid input data")
	ErrInvalidParameters    = errors.New("invalid or missing required parameters")
	ErrPasswordTooShort     = errors.New("password must be at least 8 characters long")

	// Payment & Billing Errors
	ErrPaymentFailed        = errors.New("payment processing failed")
	ErrInvalidSignature     = errors.New("payment webhook signature verification failed")
	ErrDuplicateTransaction = errors.New("duplicate transaction detected")

	// Concurrency & Idempotency Errors
	ErrIdempotencyConflict  = errors.New("concurrent request with same idempotency key in flight")
)
