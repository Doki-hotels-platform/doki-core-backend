package inventory

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"doki-backend/internal/domain"
)

// HoldSweeperService identifies expired reservation holds and releases Redis & Postgres resources.
type HoldSweeperService struct {
	reservationRepo  domain.ReservationRepository
	redisHoldAdapter domain.FastHoldPort
	logger           *slog.Logger
}

// NewHoldSweeperService initializes a hold sweeper service.
func NewHoldSweeperService(
	resRepo domain.ReservationRepository,
	redisHold domain.FastHoldPort,
	logger *slog.Logger,
) *HoldSweeperService {
	return &HoldSweeperService{
		reservationRepo:  resRepo,
		redisHoldAdapter: redisHold,
		logger:           logger,
	}
}

// SweepExpiredHolds polls expired preliminary reservations, reconciles Redis tokens, and marks PostgreSQL rows as EXPIRED.
func (s *HoldSweeperService) SweepExpiredHolds(ctx context.Context, batchSize int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if batchSize <= 0 {
		batchSize = 100
	}

	start := time.Now()
	expiredHolds, err := s.reservationRepo.GetExpiredHolds(ctx, time.Now().UTC(), batchSize)
	if err != nil {
		return 0, fmt.Errorf("fetch expired holds: %w", err)
	}

	if len(expiredHolds) == 0 {
		return 0, nil
	}

	var sweptCount int

	for _, res := range expiredHolds {
		// 1. Reconcile Redis Hold Engine (Release capacity back to fast-path pool)
		if res.HoldToken != nil && *res.HoldToken != "" && s.redisHoldAdapter != nil {
			if relErr := s.redisHoldAdapter.ReleaseFastHold(ctx, *res.HoldToken); relErr != nil {
				s.logger.Warn("releasing fast hold in Redis failed (token may have already TTL-expired)",
					slog.String("reservation_id", res.ID.String()),
					slog.String("token", *res.HoldToken),
					slog.String("error", relErr.Error()),
				)
			}
		}

		// 2. Mark reservation status as EXPIRED in authoritative database
		if expErr := s.reservationRepo.MarkReservationExpired(ctx, res.ID); expErr != nil {
			s.logger.Error("failed to mark reservation expired in database",
				slog.String("reservation_id", res.ID.String()),
				slog.String("error", expErr.Error()),
			)
			continue
		}

		sweptCount++
	}

	duration := time.Since(start)
	s.logger.Info("completed hold expiration sweep",
		slog.Int("expired_holds_found", len(expiredHolds)),
		slog.Int("holds_reconciled", sweptCount),
		slog.Duration("duration", duration),
	)

	return sweptCount, nil
}
