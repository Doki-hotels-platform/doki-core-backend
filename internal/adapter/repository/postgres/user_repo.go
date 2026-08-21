package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"doki-backend/internal/domain"
	"doki-backend/internal/domain/identity"
)

// UserRepository implements domain.UserRepository using PostgreSQL.
type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) CreateUser(ctx context.Context, u *identity.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}

	query := `
		INSERT INTO identity.app_user (
			id, phone_number, email, password_hash, full_name, role, region, is_active, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW()
		);
	`

	_, err := r.pool.Exec(
		ctx, query,
		u.ID, u.PhoneNumber, u.Email, u.PasswordHash, u.FullName, u.Role, u.Region, u.IsActive,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return domain.ErrConflict
		}
		return fmt.Errorf("insert app_user: %w", err)
	}

	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	query := `
		SELECT id, phone_number, email, password_hash, full_name, role, region, is_active, created_at, updated_at
		FROM identity.app_user
		WHERE id = $1;
	`
	return r.scanUser(r.pool.QueryRow(ctx, query, id))
}

func (r *UserRepository) GetByPhone(ctx context.Context, phone string) (*identity.User, error) {
	query := `
		SELECT id, phone_number, email, password_hash, full_name, role, region, is_active, created_at, updated_at
		FROM identity.app_user
		WHERE phone_number = $1;
	`
	return r.scanUser(r.pool.QueryRow(ctx, query, phone))
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	query := `
		SELECT id, phone_number, email, password_hash, full_name, role, region, is_active, created_at, updated_at
		FROM identity.app_user
		WHERE email = $1;
	`
	return r.scanUser(r.pool.QueryRow(ctx, query, email))
}

func (r *UserRepository) GetUserPropertyAssignments(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	query := `
		SELECT property_id
		FROM property.user_property_assignment
		WHERE user_id = $1;
	`

	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("query user property assignments: %w", err)
	}
	defer rows.Close()

	var propIDs []uuid.UUID
	for rows.Next() {
		var pid uuid.UUID
		if err := rows.Scan(&pid); err != nil {
			return nil, fmt.Errorf("scan property assignment: %w", err)
		}
		propIDs = append(propIDs, pid)
	}

	return propIDs, nil
}

func (r *UserRepository) AssignUserToProperty(ctx context.Context, userID, propertyID uuid.UUID) error {
	query := `
		INSERT INTO property.user_property_assignment (user_id, property_id)
		VALUES ($1, $2)
		ON CONFLICT (user_id, property_id) DO NOTHING;
	`

	_, err := r.pool.Exec(ctx, query, userID, propertyID)
	if err != nil {
		return fmt.Errorf("assign user to property: %w", err)
	}
	return nil
}

func (r *UserRepository) scanUser(row pgx.Row) (*identity.User, error) {
	var u identity.User
	var roleStr string
	err := row.Scan(
		&u.ID, &u.PhoneNumber, &u.Email, &u.PasswordHash,
		&u.FullName, &roleStr, &u.Region, &u.IsActive,
		&u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	u.Role = identity.Role(roleStr)
	return &u, nil
}
