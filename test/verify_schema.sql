-- ============================================================
-- DOKI Hotels Phase 1 Schema & Hard Constraint Verification
-- ============================================================

\echo '=== 1. Checking Extensions ==='
SELECT extname, extversion FROM pg_extension WHERE extname IN ('pgcrypto', 'btree_gist');

\echo '=== 2. Checking Custom Schemas ==='
SELECT schema_name FROM information_schema.schemata WHERE schema_name IN ('identity', 'property', 'inventory');

\echo '=== 3. Checking Enums ==='
SELECT n.nspname AS schema, t.typname AS enum_name, e.enumlabel AS value
FROM pg_type t
JOIN pg_enum e ON t.oid = e.enumtypid
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname IN ('identity', 'property')
ORDER BY schema, enum_name, e.enumsortorder;

\echo '=== 4. Checking Tables and Columns ==='
SELECT table_schema, table_name, column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema IN ('identity', 'property', 'inventory')
ORDER BY table_schema, table_name, ordinal_position;

\echo '=== 5. Checking Check Constraints & Foreign Keys ==='
SELECT conname AS constraint_name, contype, relname AS table_name
FROM pg_constraint c
JOIN pg_class cl ON cl.oid = c.conrelid
JOIN pg_namespace n ON n.oid = cl.relnamespace
WHERE n.nspname IN ('identity', 'property', 'inventory');

\echo '=== 6. Executing Hard Constraint chk_inventory_bounds Test ==='
DO $$
DECLARE
    v_owner_id UUID;
    v_prop_id UUID;
    v_room_type_id UUID;
    v_stay_date DATE := CURRENT_DATE + INTERVAL '7 days';
    v_error_caught BOOLEAN := FALSE;
BEGIN
    -- 1. Insert Owner User
    INSERT INTO identity.app_user (phone_number, full_name, role)
    VALUES ('+251911000001', 'Test Owner', 'HOTEL_OWNER')
    RETURNING id INTO v_owner_id;

    -- 2. Insert Property
    INSERT INTO property.property (code, name, address, city, region, owner_id)
    VALUES ('DOKI-VERIFY-01', 'Verification Grand Hotel', 'Bole Road', 'Addis Ababa', 'Addis Ababa', v_owner_id)
    RETURNING id INTO v_prop_id;

    -- 3. Insert Room Type (total capacity = 5)
    INSERT INTO property.room_type (property_id, code, name, capacity, base_rate_minor, total_inventory)
    VALUES (v_prop_id, 'DLX', 'Deluxe King', 2, 450000, 5)
    RETURNING id INTO v_room_type_id;

    -- 4. Insert Daily Allocation (total_units=5, allocated_count=4, blocked_count=0) -> Valid
    INSERT INTO inventory.daily_allocations (
        property_id, room_type_id, stay_date, total_units, allocated_count, blocked_count, rate_minor
    ) VALUES (
        v_prop_id, v_room_type_id, v_stay_date, 5, 4, 0, 450000
    );

    RAISE NOTICE 'Sample valid allocation row inserted successfully (total_units=5, allocated_count=4)';

    -- 5. Attempt invalid UPDATE pushing allocated_count to 6 (allocated_count 6 + blocked_count 0 > total_units 5)
    BEGIN
        UPDATE inventory.daily_allocations
        SET allocated_count = 6
        WHERE property_id = v_prop_id AND room_type_id = v_room_type_id AND stay_date = v_stay_date;

        -- If it reaches here, the constraint failed!
        RAISE EXCEPTION 'CRITICAL FAILURE: chk_inventory_bounds constraint did NOT prevent oversell!';
    EXCEPTION
        WHEN check_violation THEN
            v_error_caught := TRUE;
            RAISE NOTICE 'SUCCESS: PostgreSQL correctly rejected oversell with check_violation (SQLSTATE 23514) on chk_inventory_bounds!';
    END;

    IF NOT v_error_caught THEN
        RAISE EXCEPTION 'Constraint validation failed: check_violation was not triggered';
    END IF;

    -- Clean up test data
    DELETE FROM property.property WHERE id = v_prop_id;
    DELETE FROM identity.app_user WHERE id = v_owner_id;
    RAISE NOTICE 'Test cleanup completed. All verification checks PASSED.';
END $$;
