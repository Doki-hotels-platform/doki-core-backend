-- ============================================================
-- 000002_init_property_schema.down.sql
-- ============================================================

DROP TABLE IF EXISTS property.room CASCADE;
DROP TABLE IF EXISTS property.room_type CASCADE;
DROP TABLE IF EXISTS property.user_property_assignment CASCADE;
DROP INDEX IF EXISTS property.idx_property_region_status;
DROP TABLE IF EXISTS property.property CASCADE;
DROP TYPE IF EXISTS property.property_status;
DROP TYPE IF EXISTS property.property_category;
DROP SCHEMA IF EXISTS property CASCADE;
