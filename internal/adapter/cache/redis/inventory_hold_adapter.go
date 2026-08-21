package redis

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"doki-backend/internal/domain"
)

//go:embed scripts/inventory_hold.lua
var inventoryHoldLua string

//go:embed scripts/inventory_release.lua
var inventoryReleaseLua string

// InventoryHoldAdapter implements domain.FastHoldPort using Redis Lua atomic operations.
type InventoryHoldAdapter struct {
	client     *redis.Client
	holdSHA    string
	releaseSHA string
}

// NewInventoryHoldAdapter initializes and pre-loads Lua scripts into Redis SHA cache.
func NewInventoryHoldAdapter(client *redis.Client) (*InventoryHoldAdapter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	holdSHA, err := client.ScriptLoad(ctx, inventoryHoldLua).Result()
	if err != nil {
		return nil, fmt.Errorf("load inventory_hold.lua: %w", err)
	}

	releaseSHA, err := client.ScriptLoad(ctx, inventoryReleaseLua).Result()
	if err != nil {
		return nil, fmt.Errorf("load inventory_release.lua: %w", err)
	}

	return &InventoryHoldAdapter{
		client:     client,
		holdSHA:    holdSHA,
		releaseSHA: releaseSHA,
	}, nil
}

// AcquireFastHold executes Layer 1 atomic capacity check-and-decrement across all stay dates.
// If any single night in a multi-night stay fails, it compensates by immediately rolling back
// all previously decremented nights for this token, returning domain.ErrInventoryUnavailable.
func (a *InventoryHoldAdapter) AcquireFastHold(
	ctx context.Context,
	propertyID, roomTypeID uuid.UUID,
	checkIn, checkOut time.Time,
	totalCapacity int,
	ttl time.Duration,
) (string, time.Time, error) {
	if !checkOut.After(checkIn) {
		return "", time.Time{}, domain.ErrInvalidDateRange
	}

	token := "hold:tok:" + uuid.New().String()
	ttlSeconds := int(ttl.Seconds())
	if ttlSeconds <= 0 {
		ttlSeconds = 900 // 15 minutes default
	}

	expiresAt := time.Now().UTC().Add(ttl)

	// Iterate chronologically through all nights in the stay range
	for d := checkIn; d.Before(checkOut); d = d.Add(24 * time.Hour) {
		dateStr := d.Format("2006-01-02")
		holdKey := fmt.Sprintf("inv:hold:%s:%s:%s", propertyID.String(), roomTypeID.String(), dateStr)

		res, err := a.client.EvalSha(
			ctx,
			a.holdSHA,
			[]string{holdKey},
			token,
			ttlSeconds,
			totalCapacity,
		).Result()

		if err != nil {
			// Fallback to raw script evaluation if SHA was flushed from Redis
			res, err = a.client.Eval(
				ctx,
				inventoryHoldLua,
				[]string{holdKey},
				token,
				ttlSeconds,
				totalCapacity,
			).Result()
		}

		if err != nil {
			// On unexpected Redis failure, rollback any nights acquired so far
			_ = a.ReleaseFastHold(context.Background(), token)
			return "", time.Time{}, fmt.Errorf("redis eval hold on date %s: %w", dateStr, err)
		}

		success, ok := res.(int64)
		if !ok || success != 1 {
			// Capacity exhausted for this date -> Rollback prior nights under this token
			_ = a.ReleaseFastHold(context.Background(), token)
			return "", time.Time{}, domain.ErrInventoryUnavailable
		}
	}

	return token, expiresAt, nil
}

// ReleaseFastHold executes inventory_release.lua to atomically decrement all date keys
// tracked in the token's RPUSH list and delete the token index.
func (a *InventoryHoldAdapter) ReleaseFastHold(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	_, err := a.client.EvalSha(ctx, a.releaseSHA, []string{}, token).Result()
	if err != nil {
		// Fallback to raw script evaluation if SHA not present
		_, err = a.client.Eval(ctx, inventoryReleaseLua, []string{}, token).Result()
	}

	if err != nil && !errors.Is(err, redis.Nil) {
		return fmt.Errorf("release fast hold (token %s): %w", token, err)
	}

	return nil
}
