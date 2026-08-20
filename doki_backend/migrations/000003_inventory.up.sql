CREATE SCHEMA IF NOT EXISTS inventory;

-- ============================================================
-- INVENTORY SCHEMA — aggregate capacity model, the primary
-- correctness guard for the booking-time hold/confirm path
-- ============================================================
CREATE TABLE inventory.daily_allocations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    room_type_id UUID NOT NULL REFERENCES property.room_type(id) ON DELETE CASCADE,
    stay_date DATE NOT NULL,
    total_units INT NOT NULL,
    allocated_count INT NOT NULL DEFAULT 0,
    blocked_count INT NOT NULL DEFAULT 0, -- maintenance / manual HQ override
    rate_minor BIGINT NOT NULL,
    CONSTRAINT chk_inventory_bounds CHECK (
        allocated_count >= 0
        AND blocked_count >= 0
        AND allocated_count + blocked_count <= total_units
    ),
    UNIQUE (room_type_id, stay_date)
);

CREATE INDEX idx_daily_allocations_lookup
    ON inventory.daily_allocations (property_id, stay_date, room_type_id);

CREATE INDEX idx_daily_allocations_date
    ON inventory.daily_allocations (stay_date);
