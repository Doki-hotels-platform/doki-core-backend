package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/domain"
)

// ReservationRepository implements domain.ReservationRepository using PostgreSQL.
type ReservationRepository struct {
	pool *pgxpool.Pool
}

// NewReservationRepository initializes a PostgreSQL reservation repository.
func NewReservationRepository(pool *pgxpool.Pool) *ReservationRepository {
	return &ReservationRepository{pool: pool}
}

// CreateHold persists a short-lived reservation hold record.
func (r *ReservationRepository) CreateHold(ctx context.Context, hold *domain.InventoryHold, guestName, guestPhone string) (uuid.UUID, error) {
	id := uuid.New()
	ref := generateBookingReference()
	now := time.Now().UTC()

	query := `
		INSERT INTO reservation.reservations (
			id, booking_reference, property_id, room_type_id,
			guest_name, guest_phone, check_in_date, check_out_date,
			status, hold_token, hold_expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		);
	`

	_, err := r.pool.Exec(
		ctx, query,
		id, ref, hold.PropertyID, hold.RoomTypeID,
		guestName, guestPhone, hold.CheckIn.Format("2006-01-02"), hold.CheckOut.Format("2006-01-02"),
		"INVENTORY_HOLD", hold.Token, hold.ExpiresAt, now, now,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return uuid.Nil, domain.ErrConflict
		}
		return uuid.Nil, fmt.Errorf("insert reservation hold: %w", err)
	}

	return id, nil
}

// CreateReservation inserts a full booking aggregate.
func (r *ReservationRepository) CreateReservation(ctx context.Context, res *domain.Reservation) error {
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	if res.BookingReference == "" {
		res.BookingReference = generateBookingReference()
	}
	if res.CreatedAt.IsZero() {
		res.CreatedAt = time.Now().UTC()
	}
	res.UpdatedAt = res.CreatedAt

	query := `
		INSERT INTO reservation.reservations (
			id, booking_reference, property_id, room_type_id,
			guest_name, guest_phone, check_in_date, check_out_date,
			status, hold_token, hold_expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, $10, $11, $12
		);
	`

	_, err := r.pool.Exec(
		ctx, query,
		res.ID, res.BookingReference, res.PropertyID, res.RoomTypeID,
		res.GuestName, res.GuestPhone, res.CheckInDate.Format("2006-01-02"), res.CheckOutDate.Format("2006-01-02"),
		res.Status, res.HoldExpiresAt, res.CreatedAt, res.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert reservation: %w", err)
	}

	return nil
}

// GetByID retrieves a reservation record by primary key UUID.
func (r *ReservationRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Reservation, error) {
	query := `
		SELECT 
			id, booking_reference, property_id, room_type_id,
			guest_name, guest_phone, check_in_date, check_out_date,
			status, hold_expires_at, created_at, updated_at
		FROM reservation.reservations
		WHERE id = $1;
	`

	var res domain.Reservation
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&res.ID, &res.BookingReference, &res.PropertyID, &res.RoomTypeID,
		&res.GuestName, &res.GuestPhone, &res.CheckInDate, &res.CheckOutDate,
		&res.Status, &res.HoldExpiresAt, &res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrReservationNotFound
		}
		return nil, fmt.Errorf("get reservation by id: %w", err)
	}

	return &res, nil
}

// GetByReference retrieves a reservation by unique booking reference.
func (r *ReservationRepository) GetByReference(ctx context.Context, ref string) (*domain.Reservation, error) {
	query := `
		SELECT 
			id, booking_reference, property_id, room_type_id,
			guest_name, guest_phone, check_in_date, check_out_date,
			status, hold_expires_at, created_at, updated_at
		FROM reservation.reservations
		WHERE booking_reference = $1;
	`

	var res domain.Reservation
	err := r.pool.QueryRow(ctx, query, ref).Scan(
		&res.ID, &res.BookingReference, &res.PropertyID, &res.RoomTypeID,
		&res.GuestName, &res.GuestPhone, &res.CheckInDate, &res.CheckOutDate,
		&res.Status, &res.HoldExpiresAt, &res.CreatedAt, &res.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrReservationNotFound
		}
		return nil, fmt.Errorf("get reservation by reference: %w", err)
	}

	return &res, nil
}

// UpdateStatus performs an atomic reservation status transition with optimistic guard.
func (r *ReservationRepository) UpdateStatus(ctx context.Context, id uuid.UUID, oldStatus, newStatus string) error {
	var query string
	var cmdTag pgconn.CommandTag
	var err error

	if oldStatus != "" {
		query = `
			UPDATE reservation.reservations
			SET status = $1, updated_at = NOW()
			WHERE id = $2 AND status = $3;
		`
		cmdTag, err = r.pool.Exec(ctx, query, newStatus, id, oldStatus)
	} else {
		query = `
			UPDATE reservation.reservations
			SET status = $1, updated_at = NOW()
			WHERE id = $2;
		`
		cmdTag, err = r.pool.Exec(ctx, query, newStatus, id)
	}

	if err != nil {
		return fmt.Errorf("update reservation status: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		// Check if record exists
		_, fetchErr := r.GetByID(ctx, id)
		if fetchErr != nil {
			return fetchErr
		}
		return domain.ErrInvalidStatusChange
	}

	return nil
}

// GetExpiredHolds fetches pending holds whose expiration timestamp has passed.
// Uses FOR UPDATE SKIP LOCKED to prevent race conditions and lock contention across worker replicas.
func (r *ReservationRepository) GetExpiredHolds(ctx context.Context, cutoff time.Time, limit int) ([]*domain.Reservation, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT 
			id, booking_reference, property_id, room_type_id,
			guest_name, guest_phone, check_in_date, check_out_date,
			status, hold_token, hold_expires_at, created_at, updated_at
		FROM reservation.reservations
		WHERE status = 'INVENTORY_HOLD' AND hold_expires_at <= $1
		ORDER BY hold_expires_at ASC
		LIMIT $2
		FOR UPDATE SKIP LOCKED;
	`

	rows, err := r.pool.Query(ctx, query, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("query expired holds: %w", err)
	}
	defer rows.Close()

	var results []*domain.Reservation
	for rows.Next() {
		var res domain.Reservation
		if err := rows.Scan(
			&res.ID, &res.BookingReference, &res.PropertyID, &res.RoomTypeID,
			&res.GuestName, &res.GuestPhone, &res.CheckInDate, &res.CheckOutDate,
			&res.Status, &res.HoldToken, &res.HoldExpiresAt, &res.CreatedAt, &res.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan expired hold row: %w", err)
		}
		results = append(results, &res)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired holds rows error: %w", err)
	}

	return results, nil
}

// MarkReservationExpired marks a reservation as EXPIRED.
func (r *ReservationRepository) MarkReservationExpired(ctx context.Context, id uuid.UUID) error {
	query := `
		UPDATE reservation.reservations
		SET status = 'EXPIRED', updated_at = NOW()
		WHERE id = $1;
	`

	cmdTag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("mark reservation expired: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrReservationNotFound
	}

	return nil
}

func generateBookingReference() string {
	const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	result := make([]byte, 8)
	for i := range result {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		result[i] = charset[num.Int64()]
	}
	return "DK-" + string(result)
}
