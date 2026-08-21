-- ============================================================
-- 000002_init_property_schema.up.sql
-- ============================================================

CREATE SCHEMA IF NOT EXISTS property;

-- Property Enums
CREATE TYPE property.property_category AS ENUM ('BRANDED', 'AFFILIATE', 'OVERFLOW');
CREATE TYPE property.property_status AS ENUM ('DRAFT', 'PENDING_VERIFICATION', 'ACTIVE', 'SUSPENDED', 'DEACTIVATED');

-- Property Master Table
CREATE TABLE property.property (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(30) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    category property.property_category NOT NULL DEFAULT 'BRANDED',
    status property.property_status NOT NULL DEFAULT 'DRAFT',
    address VARCHAR(255) NOT NULL,
    city VARCHAR(100) NOT NULL,
    region VARCHAR(100) NOT NULL,
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),
    base_currency VARCHAR(3) NOT NULL DEFAULT 'ETB',
    check_in_time TIME NOT NULL DEFAULT '14:00:00',
    check_out_time TIME NOT NULL DEFAULT '11:00:00',
    owner_id UUID REFERENCES identity.app_user(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_property_region_status ON property.property (region, status);

-- Scopes staff/owners to the properties they operate
CREATE TABLE property.user_property_assignment (
    user_id UUID NOT NULL REFERENCES identity.app_user(id) ON DELETE CASCADE,
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, property_id)
);

-- Room Type Definition
CREATE TABLE property.room_type (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    code VARCHAR(10) NOT NULL,
    name VARCHAR(100) NOT NULL,
    capacity SMALLINT NOT NULL DEFAULT 2,
    base_rate_minor BIGINT NOT NULL, -- minor currency units (e.g., cents, santim)
    total_inventory INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (property_id, code)
);

-- Physical Room Units
CREATE TABLE property.room (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    room_type_id UUID NOT NULL REFERENCES property.room_type(id) ON DELETE RESTRICT,
    room_number VARCHAR(20) NOT NULL,
    floor VARCHAR(10),
    is_operational BOOLEAN NOT NULL DEFAULT true,
    is_clean BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (property_id, room_number)
);
