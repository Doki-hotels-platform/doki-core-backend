-- ============================================================
-- 000004_init_reservation_schema.up.sql
-- ============================================================

CREATE SCHEMA IF NOT EXISTS reservation;

CREATE TABLE IF NOT EXISTS reservation.reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_reference VARCHAR(32) UNIQUE NOT NULL,
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    room_type_id UUID NOT NULL REFERENCES property.room_type(id) ON DELETE RESTRICT,
    guest_name VARCHAR(150) NOT NULL,
    guest_phone VARCHAR(20) NOT NULL,
    check_in_date DATE NOT NULL,
    check_out_date DATE NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'INVENTORY_HOLD',
    hold_token VARCHAR(255),
    hold_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reservations_property_dates 
    ON reservation.reservations(property_id, check_in_date, check_out_date);

CREATE INDEX IF NOT EXISTS idx_reservations_status 
    ON reservation.reservations(status);

CREATE INDEX IF NOT EXISTS idx_reservations_guest_phone 
    ON reservation.reservations(guest_phone);
