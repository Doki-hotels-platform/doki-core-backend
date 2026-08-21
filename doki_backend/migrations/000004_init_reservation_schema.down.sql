-- ============================================================
-- 000004_init_reservation_schema.down.sql
-- ============================================================

DROP INDEX IF EXISTS reservation.idx_reservations_guest_phone;
DROP INDEX IF EXISTS reservation.idx_reservations_status;
DROP INDEX IF EXISTS reservation.idx_reservations_property_dates;
DROP TABLE IF EXISTS reservation.reservations CASCADE;
DROP SCHEMA IF EXISTS reservation CASCADE;
