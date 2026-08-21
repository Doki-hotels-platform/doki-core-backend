-- ============================================================
-- 000001_init_identity_schema.up.sql
-- ============================================================

-- PostgreSQL Extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "btree_gist";

-- Identity Schema
CREATE SCHEMA IF NOT EXISTS identity;

-- User Roles Enum
CREATE TYPE identity.user_role AS ENUM (
    'HQ_ADMIN',
    'REGIONAL_SUPERVISOR',
    'HOTEL_OWNER',
    'HOTEL_MANAGER',
    'RECEPTIONIST',
    'CUSTOMER',
    'CORPORATE'
);

-- App User Table
CREATE TABLE identity.app_user (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone_number VARCHAR(20) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE,
    password_hash VARCHAR(255),
    full_name VARCHAR(100) NOT NULL,
    role identity.user_role NOT NULL,
    region VARCHAR(100), -- populated for REGIONAL_SUPERVISOR
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Performance & Lookup Indexes
CREATE INDEX idx_app_user_role ON identity.app_user(role);
CREATE INDEX idx_app_user_phone ON identity.app_user(phone_number);
