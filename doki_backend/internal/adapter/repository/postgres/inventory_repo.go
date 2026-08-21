package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/domain"
	"doki-backend/pkg/types"
)

// InventoryRepository implements domain.InventoryRepository using PostgreSQL pgxpool.
type InventoryRepository struct {
	pool *pgxpool.Pool
}

// NewInventoryRepository initializes a PostgreSQL inventory repository.
func NewInventoryRepository(pool *pgxpool.Pool) *InventoryRepository {
	return &InventoryRepository{pool: pool}
}

// AcquireHold validates available capacity in PostgreSQL as part of Layer 2 checks.
func (r *InventoryRepository) AcquireHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (string, time.Time, error) {
	if !checkOut.After(checkIn) {
		return "", time.Time{}, domain.ErrInvalidDateRange
	}

	allocs, err := r.GetDailyAllocations(ctx, propertyID, roomTypeID, checkIn, checkOut)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("check capacity: %w", err)
	}

	expectedNights := int(checkOut.Sub(checkIn).Hours() / 24)
	if len(allocs) < expectedNights {
		return "", time.Time{}, domain.ErrInventoryUnavailable
	}

	for _, a := range allocs {
		if (a.AllocatedCount + a.BlockedCount + 1) > a.TotalUnits {
			return "", time.Time{}, domain.ErrInventoryUnavailable
		}
	}

	token := fmt.Sprintf("hold:%s:%s:%s", propertyID.String()[:8], roomTypeID.String()[:8], uuid.New().String()[:8])
	expiresAt := time.Now().UTC().Add(ttl)

	return token, expiresAt, nil
}

// ReleaseHold releases or rolls back hold reservations in Layer 2.
func (r *InventoryRepository) ReleaseHold(ctx context.Context, token string) error {
	// Handled authoritatively by PostgreSQL transaction rollback or cancellation workflows
	return nil
}

// CommitAllocation executes Authoritative Layer 2 PostgreSQL Capacity Validation.
// It opens a transaction, locks rows using SELECT ... FOR UPDATE ordered by stay_date ASC,
// checks capacity bounds, and increments allocated_count atomically.
func (r *InventoryRepository) CommitAllocation(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error {
	if !checkOut.After(checkIn) {
		return domain.ErrInvalidDateRange
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin allocation transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	startDateStr := checkIn.Format("2006-01-02")
	endDateStr := checkOut.Format("2006-01-02")

	// Critical Concurrency Rule: Sort rows strictly by stay_date ASC to prevent deadlocks across concurrent bookings
	selectQuery := `
		SELECT id, stay_date, total_units, allocated_count, blocked_count
		FROM inventory.daily_allocations
		WHERE property_id = $1 AND room_type_id = $2 AND stay_date >= $3 AND stay_date < $4
		ORDER BY stay_date ASC
		FOR UPDATE;
	`

	rows, err := tx.Query(ctx, selectQuery, propertyID, roomTypeID, startDateStr, endDateStr)
	if err != nil {
		return fmt.Errorf("query daily allocations for update: %w", err)
	}
	defer rows.Close()

	type record struct {
		id             uuid.UUID
		stayDate       time.Time
		totalUnits     int
		allocatedCount int
		blockedCount   int
	}

	var records []record
	for rows.Next() {
		var rec record
		if err := rows.Scan(&rec.id, &rec.stayDate, &rec.totalUnits, &rec.allocatedCount, &rec.blockedCount); err != nil {
			return fmt.Errorf("scan daily allocation row: %w", err)
		}

		// Application-layer bounds check
		if (rec.allocatedCount + rec.blockedCount + 1) > rec.totalUnits {
			return domain.ErrInventoryUnavailable
		}
		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("rows iteration: %w", err)
	}

	expectedNights := int(checkOut.Sub(checkIn).Hours() / 24)
	if len(records) < expectedNights {
		return domain.ErrInventoryUnavailable
	}

	// Atomic update incrementing allocated_count
	updateQuery := `
		UPDATE inventory.daily_allocations
		SET allocated_count = allocated_count + 1
		WHERE property_id = $1 AND room_type_id = $2 AND stay_date >= $3 AND stay_date < $4;
	`

	cmdTag, err := tx.Exec(ctx, updateQuery, propertyID, roomTypeID, startDateStr, endDateStr)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23514" { // check_violation (chk_inventory_bounds)
			return domain.ErrInventoryUnavailable
		}
		return fmt.Errorf("update daily allocations: %w", err)
	}

	if int(cmdTag.RowsAffected()) != expectedNights {
		return fmt.Errorf("unexpected rows affected: expected %d, got %d", expectedNights, cmdTag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit allocation transaction: %w", err)
	}

	return nil
}

// GetDailyAllocations retrieves availability records for search and pricing calculations.
func (r *InventoryRepository) GetDailyAllocations(ctx context.Context, propertyID, roomTypeID uuid.UUID, startDate, endDate time.Time) ([]*domain.DailyAllocation, error) {
	query := `
		SELECT id, property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
		FROM inventory.daily_allocations
		WHERE property_id = $1 AND room_type_id = $2 AND stay_date >= $3 AND stay_date < $4
		ORDER BY stay_date ASC;
	`

	rows, err := r.pool.Query(ctx, query, propertyID, roomTypeID, startDate.Format("2006-01-02"), endDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("query daily allocations: %w", err)
	}
	defer rows.Close()

	var results []*domain.DailyAllocation
	for rows.Next() {
		var a domain.DailyAllocation
		var rateMinor int64
		if err := rows.Scan(&a.ID, &a.PropertyID, &a.RoomTypeID, &a.StayDate, &a.TotalUnits, &a.AllocatedCount, &a.BlockedCount, &rateMinor); err != nil {
			return nil, fmt.Errorf("scan daily allocation: %w", err)
		}
		a.RateMinor = types.NewMoney(rateMinor, "ETB")
		results = append(results, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("daily allocations rows error: %w", err)
	}

	return results, nil
}

// BatchUpsertDailyAllocations executes high-performance batch upsert using a single transaction.
// It inserts new daily allocation slices and updates total_units and rate_minor on conflict,
// strictly protecting allocated_count and blocked_count from being overwritten if reservations exist.
func (r *InventoryRepository) BatchUpsertDailyAllocations(ctx context.Context, allocations []*domain.DailyAllocation) error {
	if len(allocations) == 0 {
		return nil
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin batch upsert tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	batch := &pgx.Batch{}
	upsertQuery := `
		INSERT INTO inventory.daily_allocations (
			id, property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		ON CONFLICT (property_id, room_type_id, stay_date)
		DO UPDATE SET
			total_units = EXCLUDED.total_units,
			rate_minor = EXCLUDED.rate_minor
		WHERE inventory.daily_allocations.allocated_count = 0;
	`

	for _, a := range allocations {
		id := a.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		batch.Queue(
			upsertQuery,
			id, a.PropertyID, a.RoomTypeID,
			a.StayDate.Format("2006-01-02"),
			a.TotalUnits, a.AllocatedCount, a.BlockedCount,
			a.RateMinor.AmountMinor,
		)
	}

	br := tx.SendBatch(ctx, batch)
	for i := 0; i < len(allocations); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return fmt.Errorf("exec batch item %d: %w", i, err)
		}
	}

	if err := br.Close(); err != nil {
		return fmt.Errorf("close batch results: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit batch upsert tx: %w", err)
	}

	return nil
}
