package worker

import (
	"context"
	"log/slog"
	"time"

	"doki-backend/internal/domain/inventory"
	"doki-backend/internal/platform/telemetry"
)

// HoldExpiryWorker runs on a high-frequency ticker to identify expired holds and reconcile Redis.
type HoldExpiryWorker struct {
	sweeper   *inventory.HoldSweeperService
	logger    *slog.Logger
	interval  time.Duration
	batchSize int
}

// NewHoldExpiryWorker initializes the hold expiry background worker.
func NewHoldExpiryWorker(
	sweeper *inventory.HoldSweeperService,
	logger *slog.Logger,
	interval time.Duration,
	batchSize int,
) *HoldExpiryWorker {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 100
	}
	return &HoldExpiryWorker{
		sweeper:   sweeper,
		logger:    logger,
		interval:  interval,
		batchSize: batchSize,
	}
}

// Run starts the high-frequency ticker loop until context cancellation.
func (w *HoldExpiryWorker) Run(ctx context.Context) error {
	w.logger.Info("starting hold expiry background reconciler worker",
		slog.Duration("interval", w.interval),
		slog.Int("batch_size", w.batchSize),
	)

	// Immediate run on startup
	w.sweep(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("stopping hold expiry reconciler worker gracefully")
			return ctx.Err()
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

// RunOnce executes a single pass of the hold expiry sweeper.
func (w *HoldExpiryWorker) RunOnce(ctx context.Context) error {
	return w.sweep(ctx)
}

func (w *HoldExpiryWorker) sweep(ctx context.Context) error {
	swept, err := w.sweeper.SweepExpiredHolds(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("hold expiry sweep cycle encountered an error", slog.String("error", err.Error()))
		return err
	}
	if swept > 0 {
		telemetry.ReservationHoldExpired.Add(float64(swept))
	}
	return nil
}
