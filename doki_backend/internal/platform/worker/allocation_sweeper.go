package worker

import (
	"context"
	"log/slog"
	"time"

	"doki-backend/internal/domain/inventory"
)

// AllocationSweeper executes the rolling 365-day inventory horizon generation on a background schedule.
type AllocationSweeper struct {
	allocService *inventory.AllocationService
	logger       *slog.Logger
	interval     time.Duration
	daysAhead    int
}

// NewAllocationSweeper initializes the background allocation generator worker.
func NewAllocationSweeper(
	allocService *inventory.AllocationService,
	logger *slog.Logger,
	interval time.Duration,
	daysAhead int,
) *AllocationSweeper {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if daysAhead <= 0 {
		daysAhead = inventory.DefaultRollingHorizonDays
	}
	return &AllocationSweeper{
		allocService: allocService,
		logger:       logger,
		interval:     interval,
		daysAhead:    daysAhead,
	}
}

// Run starts the ticker loop and runs until context cancellation.
func (s *AllocationSweeper) Run(ctx context.Context) error {
	s.logger.Info("starting daily allocation sweeper worker",
		slog.Duration("interval", s.interval),
		slog.Int("days_ahead", s.daysAhead),
	)

	// Immediate run on startup
	s.sweep(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("stopping daily allocation sweeper worker gracefully")
			return ctx.Err()
		case <-ticker.C:
			s.sweep(ctx)
		}
	}
}

// RunOnce executes a single pass of the allocation generator (useful for cron jobs or test runners).
func (s *AllocationSweeper) RunOnce(ctx context.Context) error {
	return s.sweep(ctx)
}

func (s *AllocationSweeper) sweep(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("commencing daily allocation replenishment sweep...")

	props, roomTypes, allocs, err := s.allocService.ProcessAllActiveProperties(ctx, s.daysAhead)
	duration := time.Since(start)

	if err != nil {
		s.logger.Error("allocation sweeper run failed",
			slog.String("error", err.Error()),
			slog.Duration("duration", duration),
		)
		return err
	}

	s.logger.Info("allocation replenishment sweep completed successfully",
		slog.Int("properties_processed", props),
		slog.Int("room_types_processed", roomTypes),
		slog.Int("daily_allocations_upserted", allocs),
		slog.Duration("duration", duration),
	)

	return nil
}
