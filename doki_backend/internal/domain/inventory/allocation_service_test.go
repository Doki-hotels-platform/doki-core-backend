package inventory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"doki-backend/internal/domain"
	"doki-backend/internal/domain/property"
	"doki-backend/pkg/types"
)

type mockAllocInvRepo struct {
	mu          sync.Mutex
	allocations map[string]*domain.DailyAllocation
}

func newMockAllocInvRepo() *mockAllocInvRepo {
	return &mockAllocInvRepo{
		allocations: make(map[string]*domain.DailyAllocation),
	}
}

func (m *mockAllocInvRepo) AcquireHold(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time, ttl time.Duration) (string, time.Time, error) {
	return "token", time.Now().Add(ttl), nil
}

func (m *mockAllocInvRepo) ReleaseHold(ctx context.Context, token string) error {
	return nil
}

func (m *mockAllocInvRepo) CommitAllocation(ctx context.Context, propertyID, roomTypeID uuid.UUID, checkIn, checkOut time.Time) error {
	return nil
}

func (m *mockAllocInvRepo) GetDailyAllocations(ctx context.Context, propertyID, roomTypeID uuid.UUID, startDate, endDate time.Time) ([]*domain.DailyAllocation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*domain.DailyAllocation
	for _, a := range m.allocations {
		if a.PropertyID == propertyID && a.RoomTypeID == roomTypeID && !a.StayDate.Before(startDate) && a.StayDate.Before(endDate) {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *mockAllocInvRepo) BatchUpsertDailyAllocations(ctx context.Context, allocations []*domain.DailyAllocation) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range allocations {
		key := a.PropertyID.String() + ":" + a.RoomTypeID.String() + ":" + a.StayDate.Format("2006-01-02")
		existing, found := m.allocations[key]
		if found {
			// In PostgreSQL: DO UPDATE SET total_units = EXCLUDED.total_units WHERE allocated_count = 0
			if existing.AllocatedCount == 0 {
				existing.TotalUnits = a.TotalUnits
				existing.RateMinor = a.RateMinor
			}
			// If allocated_count > 0, existing allocations are strictly preserved!
		} else {
			m.allocations[key] = a
		}
	}
	return nil
}

type mockAllocPropRepo struct {
	properties []*property.Property
	roomTypes  map[uuid.UUID][]*property.RoomType
}

func (m *mockAllocPropRepo) CreateProperty(ctx context.Context, p *property.Property) error {
	return nil
}
func (m *mockAllocPropRepo) GetPropertyByID(ctx context.Context, id uuid.UUID) (*property.Property, error) {
	return nil, nil
}
func (m *mockAllocPropRepo) UpdateProperty(ctx context.Context, p *property.Property) error {
	return nil
}
func (m *mockAllocPropRepo) ListProperties(ctx context.Context, filter property.PropertyFilter) ([]*property.Property, error) {
	return m.properties, nil
}
func (m *mockAllocPropRepo) CreateRoomType(ctx context.Context, rt *property.RoomType) error {
	return nil
}
func (m *mockAllocPropRepo) GetRoomTypeByID(ctx context.Context, id uuid.UUID) (*property.RoomType, error) {
	return nil, nil
}
func (m *mockAllocPropRepo) ListRoomTypesByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.RoomType, error) {
	return m.roomTypes[propertyID], nil
}
func (m *mockAllocPropRepo) CreateRoom(ctx context.Context, r *property.PhysicalRoom) error {
	return nil
}
func (m *mockAllocPropRepo) ListRoomsByProperty(ctx context.Context, propertyID uuid.UUID) ([]*property.PhysicalRoom, error) {
	return nil, nil
}

func TestGenerateRollingAllocations_Creates365Days(t *testing.T) {
	invRepo := newMockAllocInvRepo()
	propRepo := &mockAllocPropRepo{}

	service := NewAllocationService(invRepo, propRepo)

	propID := uuid.New()
	roomTypeID := uuid.New()

	err := service.GenerateRollingAllocations(context.Background(), propID, roomTypeID, 10, 500000, 365)
	if err != nil {
		t.Fatalf("expected successful allocation generation, got: %v", err)
	}

	if len(invRepo.allocations) != 365 {
		t.Fatalf("expected exactly 365 daily allocations, got: %d", len(invRepo.allocations))
	}

	// Verify sequential dates from today
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for i := 0; i < 365; i++ {
		dateStr := today.AddDate(0, 0, i).Format("2006-01-02")
		key := propID.String() + ":" + roomTypeID.String() + ":" + dateStr
		alloc, exists := invRepo.allocations[key]
		if !exists {
			t.Fatalf("expected allocation for date %s to exist", dateStr)
		}
		if alloc.TotalUnits != 10 {
			t.Errorf("expected total_units 10, got %d", alloc.TotalUnits)
		}
		if alloc.RateMinor.AmountMinor != 500000 {
			t.Errorf("expected rate 500000, got %d", alloc.RateMinor.AmountMinor)
		}
	}
}

func TestBatchUpsert_PreservesAllocatedCountOnConflict(t *testing.T) {
	invRepo := newMockAllocInvRepo()
	propRepo := &mockAllocPropRepo{}

	service := NewAllocationService(invRepo, propRepo)

	propID := uuid.New()
	roomTypeID := uuid.New()

	// 1. Initial Seeding: 10 units at rate 400,000
	_ = service.GenerateRollingAllocations(context.Background(), propID, roomTypeID, 10, 400000, 30)

	// 2. Simulate an existing reservation on Day 5 (allocated_count = 3)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day5Str := today.AddDate(0, 0, 5).Format("2006-01-02")
	day5Key := propID.String() + ":" + roomTypeID.String() + ":" + day5Str

	invRepo.allocations[day5Key].AllocatedCount = 3

	// 3. Re-run sweeper with new pricing & inventory: 15 units at rate 600,000
	err := service.GenerateRollingAllocations(context.Background(), propID, roomTypeID, 15, 600000, 30)
	if err != nil {
		t.Fatalf("second sweep failed: %v", err)
	}

	// 4. Verify Day 0 (unbooked) was updated to new capacity and rate
	day0Str := today.Format("2006-01-02")
	day0Key := propID.String() + ":" + roomTypeID.String() + ":" + day0Str
	if invRepo.allocations[day0Key].TotalUnits != 15 {
		t.Errorf("expected unbooked Day 0 total_units updated to 15, got %d", invRepo.allocations[day0Key].TotalUnits)
	}

	// 5. Verify Day 5 (booked with 3 reservations) preserved its allocated_count
	if invRepo.allocations[day5Key].AllocatedCount != 3 {
		t.Errorf("expected Day 5 allocated_count to be preserved at 3, got %d", invRepo.allocations[day5Key].AllocatedCount)
	}
}

func TestAllocationService_ProcessAllActiveProperties(t *testing.T) {
	invRepo := newMockAllocInvRepo()
	propID1 := uuid.New()
	propID2 := uuid.New()

	rt1 := &property.RoomType{ID: uuid.New(), PropertyID: propID1, TotalInventory: 5, BaseRateMinor: types.NewMoney(300000, "ETB")}
	rt2 := &property.RoomType{ID: uuid.New(), PropertyID: propID1, TotalInventory: 10, BaseRateMinor: types.NewMoney(500000, "ETB")}
	rt3 := &property.RoomType{ID: uuid.New(), PropertyID: propID2, TotalInventory: 8, BaseRateMinor: types.NewMoney(400000, "ETB")}

	propRepo := &mockAllocPropRepo{
		properties: []*property.Property{
			{ID: propID1, Name: "Hotel One", Status: property.StatusActive},
			{ID: propID2, Name: "Hotel Two", Status: property.StatusActive},
		},
		roomTypes: map[uuid.UUID][]*property.RoomType{
			propID1: {rt1, rt2},
			propID2: {rt3},
		},
	}

	service := NewAllocationService(invRepo, propRepo)

	props, roomTypes, totalAllocs, err := service.ProcessAllActiveProperties(context.Background(), 10)
	if err != nil {
		t.Fatalf("failed to process active properties: %v", err)
	}

	if props != 2 {
		t.Errorf("expected 2 properties processed, got %d", props)
	}
	if roomTypes != 3 {
		t.Errorf("expected 3 room types processed, got %d", roomTypes)
	}
	if totalAllocs != 30 {
		t.Errorf("expected 30 total daily allocations upserted, got %d", totalAllocs)
	}
}
