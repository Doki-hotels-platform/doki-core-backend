CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "btree_gist";

CREATE SCHEMA IF NOT EXISTS identity;

-- ============================================================
-- IDENTITY SCHEMA
-- ============================================================
CREATE TYPE identity.user_role AS ENUM (
    'HQ_ADMIN',
    'REGIONAL_SUPERVISOR',
    'HOTEL_OWNER',
    'HOTEL_MANAGER',
    'RECEPTIONIST',
    'CUSTOMER',
    'CORPORATE'
);

CREATE TABLE identity.app_user (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    phone_number VARCHAR(20) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE,
    password_hash VARCHAR(255) NOT NULL,
    full_name VARCHAR(150) NOT NULL,
    role identity.user_role NOT NULL,
    region VARCHAR(100), -- populated for REGIONAL_SUPERVISOR
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_app_user_role ON identity.app_user (role);
CREATE INDEX idx_app_user_phone ON identity.app_user (phone_number);
