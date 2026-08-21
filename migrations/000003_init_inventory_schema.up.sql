-- ============================================================
-- 000003_init_inventory_schema.up.sql
-- ============================================================

CREATE SCHEMA IF NOT EXISTS inventory;

-- Daily Allocations Table (Aggregate Capacity Model)
CREATE TABLE inventory.daily_allocations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    room_type_id UUID NOT NULL REFERENCES property.room_type(id) ON DELETE CASCADE,
    stay_date DATE NOT NULL,
    total_units INT NOT NULL,
    allocated_count INT NOT NULL DEFAULT 0,
    blocked_count INT NOT NULL DEFAULT 0,
    rate_minor BIGINT NOT NULL,
    CONSTRAINT chk_inventory_bounds CHECK (
        allocated_count >= 0 
        AND blocked_count >= 0 
        AND (allocated_count + blocked_count) <= total_units
    ),
    UNIQUE (property_id, room_type_id, stay_date)
);

-- Fast Index for Availability Search
CREATE INDEX idx_daily_allocations_lookup 
    ON inventory.daily_allocations(property_id, room_type_id, stay_date);

CREATE INDEX idx_daily_allocations_date 
    ON inventory.daily_allocations(stay_date);
