-- inventory_hold.lua
-- Atomically checks available capacity and records a room hold.
-- KEYS[1] = "inv:hold:{property_id}:{room_type_id}:{stay_date}"
-- ARGV[1] = hold_token
-- ARGV[2] = ttl_seconds (e.g. 600 for 10 minutes)
-- ARGV[3] = total_capacity (from inventory.daily_allocations.total_units)
--
-- Returns 1 on success, 0 if capacity is exhausted.
local current = tonumber(redis.call("GET", KEYS[1])) or 0
local capacity = tonumber(ARGV[3])

if current >= capacity then
    return 0
end

redis.call("INCR", KEYS[1])
redis.call("EXPIRE", KEYS[1], ARGV[2])

-- Multi-date index: RPUSH appends each date key to the token collection
local tokenKey = "inv:token:" .. ARGV[1]
redis.call("RPUSH", tokenKey, KEYS[1])
redis.call("EXPIRE", tokenKey, ARGV[2])

return 1
