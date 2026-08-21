package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/domain"
	"doki-backend/internal/domain/property"
	"doki-backend/pkg/types"
)

// PropertyRepository implements domain.PropertyRepository using PostgreSQL.
type PropertyRepository struct {
	pool *pgxpool.Pool
}

// NewPropertyRepository initializes a PostgreSQL property repository.
func NewPropertyRepository(pool *pgxpool.Pool) *PropertyRepository {
	return &PropertyRepository{pool: pool}
}

// CreateProperty inserts a new property profile record.
func (r *PropertyRepository) CreateProperty(ctx context.Context, p *property.Property) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}

	query := `
		INSERT INTO property.property (
			id, code, name, category, status, address, city, region,
			latitude, longitude, base_currency, check_in_time, check_out_time, owner_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::time, $13::time, $14, NOW(), NOW()
		);
	`

	if p.CheckInTime == "" {
		p.CheckInTime = "14:00:00"
	}
	if p.CheckOutTime == "" {
		p.CheckOutTime = "11:00:00"
	}
	if p.BaseCurrency == "" {
		p.BaseCurrency = "ETB"
	}

	_, err := r.pool.Exec(
		ctx, query,
		p.ID, p.Code, p.Name, string(p.Category), string(p.Status),
		"Address Pending", p.City, p.Region,
		p.Latitude, p.Longitude, p.BaseCurrency,
		p.CheckInTime, p.CheckOutTime, p.OwnerUserID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert property: %w", err)
	}

	return nil
}

// UpdateProperty updates an existing property profile and status.
func (r *PropertyRepository) UpdateProperty(ctx context.Context, p *property.Property) error {
	query := `
		UPDATE property.property
		SET name = $1, category = $2, status = $3, city = $4, region = $5,
		    latitude = $6, longitude = $7, updated_at = NOW()
		WHERE id = $8;
	`

	cmdTag, err := r.pool.Exec(
		ctx, query,
		p.Name, string(p.Category), string(p.Status), p.City, p.Region,
		p.Latitude, p.Longitude, p.ID,
	)
	if err != nil {
		return fmt.Errorf("update property: %w", err)
	}

	if cmdTag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// GetPropertyByID fetches a single property record by UUID.
func (r *PropertyRepository) GetPropertyByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	query := `
		SELECT 
			id, code, name, category, status,
			region, city, latitude, longitude,
			base_currency, check_in_time::text, check_out_time::text,
			owner_id, created_at, updated_at
		FROM property.property
		WHERE id = $1;
	`

	var p property.Property
	var catStr, statusStr string
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Code, &p.Name, &catStr, &statusStr,
		&p.Region, &p.City, &p.Latitude, &p.Longitude,
		&p.BaseCurrency, &p.CheckInTime, &p.CheckOutTime,
		&p.OwnerUserID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get property by id: %w", err)
	}
	p.Category = property.Category(catStr)
	p.Status = property.Status(statusStr)

	return &p, nil
}

// ListProperties queries properties matching multi-dimensional filters.
func (r *PropertyRepository) ListProperties(ctx context.Context, filter property.PropertyFilter) ([]*property.Property, error) {
	baseQuery := `
		SELECT 
			id, code, name, category, status,
			region, city, latitude, longitude,
			base_currency, check_in_time::text, check_out_time::text,
			owner_id, created_at, updated_at
		FROM property.property
	`

	var conditions []string
	var args []any
	argIndex := 1

	if filter.Region != nil {
		conditions = append(conditions, "region = $"+strconv.Itoa(argIndex))
		args = append(args, *filter.Region)
		argIndex++
	}

	if filter.City != nil {
		conditions = append(conditions, "city = $"+strconv.Itoa(argIndex))
		args = append(args, *filter.City)
		argIndex++
	}

	if filter.Category != nil {
		conditions = append(conditions, "category = $"+strconv.Itoa(argIndex))
		args = append(args, string(*filter.Category))
		argIndex++
	}

	if filter.Status != nil {
		conditions = append(conditions, "status = $"+strconv.Itoa(argIndex))
		args = append(args, string(*filter.Status))
		argIndex++
	}

	query := baseQuery
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY name ASC"

	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	query += " LIMIT $" + strconv.Itoa(argIndex)
	args = append(args, limit)
	argIndex++

	if filter.Offset > 0 {
		query += " OFFSET $" + strconv.Itoa(argIndex)
		args = append(args, filter.Offset)
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list properties query: %w", err)
	}
	defer rows.Close()

	var properties []*property.Property
	for rows.Next() {
		var p property.Property
		var catStr, statusStr string
		err := rows.Scan(
			&p.ID, &p.Code, &p.Name, &catStr, &statusStr,
			&p.Region, &p.City, &p.Latitude, &p.Longitude,
			&p.BaseCurrency, &p.CheckInTime, &p.CheckOutTime,
			&p.OwnerUserID, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan property: %w", err)
		}
		p.Category = property.Category(catStr)
		p.Status = property.Status(statusStr)
		properties = append(properties, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list properties rows: %w", err)
	}

	return properties, nil
}

// CreateRoomType provisions a room type category under a property.
func (r *PropertyRepository) CreateRoomType(ctx context.Context, rt *property.RoomType) error {
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}

	query := `
		INSERT INTO property.room_type (
			id, property_id, code, name, capacity, base_rate_minor, total_inventory, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NOW()
		);
	`

	_, err := r.pool.Exec(
		ctx, query,
		rt.ID, rt.PropertyID, rt.Code, rt.Name, rt.Capacity, rt.BaseRateMinor.AmountMinor, rt.TotalInventory,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert room_type: %w", err)
	}

	return nil
}

// GetRoomTypeByID retrieves a room type definition.
func (r *PropertyRepository) GetRoomTypeByID(ctx context.Context, id uuid.UUID) (*property.RoomType, error) {
	query := `
		SELECT id, property_id, code, name, capacity, base_rate_minor, total_inventory, created_at
		FROM property.room_type
		WHERE id = $1;
	`

	var rt property.RoomType
	var rateMinor int64
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&rt.ID, &rt.PropertyID, &rt.Code, &rt.Name,
		&rt.Capacity, &rateMinor, &rt.TotalInventory, &rt.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("get room type by id: %w", err)
	}
	rt.BaseRateMinor = types.NewMoney(rateMinor, "ETB")

	return &rt, nil
}

// ListRoomTypesByProperty returns all room types configured for a property.
func (r *PropertyRepository) ListRoomTypesByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.RoomType, error) {
	query := `
		SELECT id, property_id, code, name, capacity, base_rate_minor, total_inventory, created_at
		FROM property.room_type
		WHERE property_id = $1
		ORDER BY base_rate_minor ASC;
	`

	rows, err := r.pool.Query(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list room types query: %w", err)
	}
	defer rows.Close()

	var roomTypes []*property.RoomType
	for rows.Next() {
		var rt property.RoomType
		var rateMinor int64
		if err := rows.Scan(&rt.ID, &rt.PropertyID, &rt.Code, &rt.Name, &rt.Capacity, &rateMinor, &rt.TotalInventory, &rt.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan room type: %w", err)
		}
		rt.BaseRateMinor = types.NewMoney(rateMinor, "ETB")
		roomTypes = append(roomTypes, &rt)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list room types rows: %w", err)
	}

	return roomTypes, nil
}

// CreateRoom adds a physical room unit under a property and room type.
func (r *PropertyRepository) CreateRoom(ctx context.Context, rm *property.PhysicalRoom) error {
	if rm.ID == uuid.Nil {
		rm.ID = uuid.New()
	}

	query := `
		INSERT INTO property.room (
			id, property_id, room_type_id, room_number, floor, is_operational, is_clean
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		);
	`

	_, err := r.pool.Exec(
		ctx, query,
		rm.ID, rm.PropertyID, rm.RoomTypeID, rm.RoomNumber, rm.Floor, rm.IsOperational, rm.IsClean,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrConflict
		}
		return fmt.Errorf("insert room: %w", err)
	}

	return nil
}

// ListRoomsByProperty retrieves physical rooms under a property.
func (r *PropertyRepository) ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.PhysicalRoom, error) {
	query := `
		SELECT id, property_id, room_type_id, room_number, floor, is_operational, is_clean
		FROM property.room
		WHERE property_id = $1
		ORDER BY room_number ASC;
	`

	rows, err := r.pool.Query(ctx, query, propertyID)
	if err != nil {
		return nil, fmt.Errorf("list rooms query: %w", err)
	}
	defer rows.Close()

	var rooms []*property.PhysicalRoom
	for rows.Next() {
		var rm property.PhysicalRoom
		if err := rows.Scan(&rm.ID, &rm.PropertyID, &rm.RoomTypeID, &rm.RoomNumber, &rm.Floor, &rm.IsOperational, &rm.IsClean); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, &rm)
	}

	return rooms, nil
}
