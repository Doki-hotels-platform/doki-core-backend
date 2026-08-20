CREATE SCHEMA IF NOT EXISTS property;

-- ============================================================
-- PROPERTY SCHEMA
-- ============================================================
CREATE TYPE property.property_category AS ENUM ('DOKI_BRANDED', 'AFFILIATE_OVERFLOW');
CREATE TYPE property.property_status AS ENUM ('ONBOARDING', 'ACTIVE', 'SUSPENDED', 'DEACTIVATED');

CREATE TABLE property.property (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code VARCHAR(32) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    category property.property_category NOT NULL DEFAULT 'DOKI_BRANDED',
    status property.property_status NOT NULL DEFAULT 'ONBOARDING',
    region VARCHAR(100) NOT NULL,
    city VARCHAR(100) NOT NULL,
    latitude DECIMAL(10, 8),
    longitude DECIMAL(11, 8),
    base_currency CHAR(3) NOT NULL DEFAULT 'ETB',
    checkin_time TIME NOT NULL DEFAULT '14:00:00',
    checkout_time TIME NOT NULL DEFAULT '11:00:00',
    owner_user_id UUID REFERENCES identity.app_user(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_property_region_status ON property.property (region, status);

-- Scopes owner/manager/receptionist users to the properties they operate
CREATE TABLE property.user_property_assignment (
    user_id UUID NOT NULL REFERENCES identity.app_user(id) ON DELETE CASCADE,
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, property_id)
);

CREATE TABLE property.room_type (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    code VARCHAR(50) NOT NULL,
    name VARCHAR(100) NOT NULL,
    capacity SMALLINT NOT NULL DEFAULT 2,
    base_rate_minor BIGINT NOT NULL, -- integer minor currency units, never floats
    total_inventory INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (property_id, code)
);

CREATE TABLE property.room (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    property_id UUID NOT NULL REFERENCES property.property(id) ON DELETE CASCADE,
    room_type_id UUID NOT NULL REFERENCES property.room_type(id) ON DELETE RESTRICT,
    room_number VARCHAR(30) NOT NULL,
    floor VARCHAR(20),
    is_operational BOOLEAN NOT NULL DEFAULT TRUE,
    is_clean BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (property_id, room_number)
);
