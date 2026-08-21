-- inventory_release.lua
-- Atomically releases all room holds associated with a multi-date hold token.
-- ARGV[1] = hold_token
--
-- Returns 1 on success, 0 if token does not exist or was already released.
local tokenKey = "inv:token:" .. ARGV[1]
local holdKeys = redis.call("LRANGE", tokenKey, 0, -1)

if #holdKeys == 0 then
    -- Fallback compatibility for single string key format
    local singleKey = redis.call("GET", tokenKey)
    if singleKey then
        local count = tonumber(redis.call("GET", singleKey)) or 0
        if count > 0 then
            redis.call("DECR", singleKey)
        end
        redis.call("DEL", tokenKey)
        return 1
    end
    return 0 -- already released or never existed
end

for _, key in ipairs(holdKeys) do
    local count = tonumber(redis.call("GET", key)) or 0
    if count > 0 then
        redis.call("DECR", key)
    end
end

redis.call("DEL", tokenKey)
return 1
