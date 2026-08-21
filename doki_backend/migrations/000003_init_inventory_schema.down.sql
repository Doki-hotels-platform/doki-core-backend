-- ============================================================
-- 000003_init_inventory_schema.down.sql
-- ============================================================

DROP INDEX IF EXISTS inventory.idx_daily_allocations_date;
DROP INDEX IF EXISTS inventory.idx_daily_allocations_lookup;
DROP TABLE IF EXISTS inventory.daily_allocations CASCADE;
DROP SCHEMA IF EXISTS inventory CASCADE;
