package inventory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
	"doki-backend/internal/domain/property"
	"doki-backend/pkg/types"
)

const (
	DefaultRollingHorizonDays = 365
	AllocationBatchChunkSize  = 100
)

// AllocationService provisions and maintains the rolling daily inventory availability window.
type AllocationService struct {
	inventoryRepo domain.InventoryRepository
	propertyRepo  property.Repository
}

func NewAllocationService(invRepo domain.InventoryRepository, propRepo property.Repository) *AllocationService {
	return &AllocationService{
		inventoryRepo: invRepo,
		propertyRepo:  propRepo,
	}
}

// GenerateRollingAllocations creates or updates daily allocation slices for a room type.
func (s *AllocationService) GenerateRollingAllocations(
	ctx context.Context,
	propertyID, roomTypeID uuid.UUID,
	totalUnits int,
	baseRateMinor int64,
	daysAhead int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if daysAhead <= 0 {
		daysAhead = DefaultRollingHorizonDays
	}

	startDate := time.Now().UTC().Truncate(24 * time.Hour)
	rate := types.NewMoney(baseRateMinor, "ETB")

	var chunk []*domain.DailyAllocation

	for i := 0; i < daysAhead; i++ {
		stayDate := startDate.AddDate(0, 0, i)
		alloc := &domain.DailyAllocation{
			ID:             uuid.New(),
			PropertyID:     propertyID,
			RoomTypeID:     roomTypeID,
			StayDate:       stayDate,
			TotalUnits:     totalUnits,
			AllocatedCount: 0,
			BlockedCount:   0,
			RateMinor:      rate,
		}
		chunk = append(chunk, alloc)

		if len(chunk) >= AllocationBatchChunkSize || i == daysAhead-1 {
			if err := s.inventoryRepo.BatchUpsertDailyAllocations(ctx, chunk); err != nil {
				return fmt.Errorf("upsert allocations chunk (date %s): %w", stayDate.Format("2006-01-02"), err)
			}
			chunk = chunk[:0]
		}
	}

	return nil
}

// SeedNewRoomTypeInventory is triggered when a new room type is created to populate the initial 365 days.
func (s *AllocationService) SeedNewRoomTypeInventory(ctx context.Context, propertyID uuid.UUID, rt *property.RoomType) error {
	return s.GenerateRollingAllocations(
		ctx,
		propertyID,
		rt.ID,
		rt.TotalInventory,
		rt.BaseRateMinor.AmountMinor,
		DefaultRollingHorizonDays,
	)
}

// ProcessAllActiveProperties iterates over all active hotels and room types to extend their 365-day booking horizon.
func (s *AllocationService) ProcessAllActiveProperties(ctx context.Context, daysAhead int) (int, int, int, error) {
	if daysAhead <= 0 {
		daysAhead = DefaultRollingHorizonDays
	}

	activeStatus := property.StatusActive
	properties, err := s.propertyRepo.ListProperties(ctx, property.PropertyFilter{
		Status: &activeStatus,
		Limit:  1000,
	})
	if err != nil {
		return 0, 0, 0, fmt.Errorf("list active properties: %w", err)
	}

	var propCount, roomTypeCount, totalAllocations int

	for _, p := range properties {
		roomTypes, err := s.propertyRepo.ListRoomTypesByProperty(ctx, p.ID)
		if err != nil {
			return propCount, roomTypeCount, totalAllocations, fmt.Errorf("list room types for property %s: %w", p.ID, err)
		}

		if len(roomTypes) == 0 {
			continue
		}

		propCount++
		for _, rt := range roomTypes {
			if err := s.GenerateRollingAllocations(ctx, p.ID, rt.ID, rt.TotalInventory, rt.BaseRateMinor.AmountMinor, daysAhead); err != nil {
				return propCount, roomTypeCount, totalAllocations, fmt.Errorf("generate allocations for room type %s: %w", rt.ID, err)
			}
			roomTypeCount++
			totalAllocations += daysAhead
		}
	}

	return propCount, roomTypeCount, totalAllocations, nil
}
