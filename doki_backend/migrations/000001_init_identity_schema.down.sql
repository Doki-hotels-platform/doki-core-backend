-- ============================================================
-- 000001_init_identity_schema.down.sql
-- ============================================================

DROP INDEX IF EXISTS identity.idx_app_user_phone;
DROP INDEX IF EXISTS identity.idx_app_user_role;
DROP TABLE IF EXISTS identity.app_user CASCADE;
DROP TYPE IF EXISTS identity.user_role;
DROP SCHEMA IF EXISTS identity CASCADE;
