-- rate_limit.lua
-- OpenResty access phase script for per-IP rate limiting
-- Uses lua_shared_dict for fast, in-process rate limiting
-- (No Redis round-trip needed for proxy-level rate limiting)

local limit_store = ngx.shared.rate_limit_store

-- Configuration
local PROXY_RATE_LIMIT = PROXY_RATE_LIMIT_RPM or 600  -- from env, default 600 req/min
local WINDOW_SECONDS = 60     -- sliding window size

-- Get client IP (trust X-Forwarded-For since we're the edge)
local client_ip = ngx.var.remote_addr

-- Build rate limit key
local key = "proxy:" .. client_ip

-- Get current count
local count, err = limit_store:get(key)

if count == nil then
    -- First request from this IP in this window
    count = 0
end

-- Increment
count = count + 1

if count == 1 then
    -- First request - set with TTL
    limit_store:set(key, count, WINDOW_SECONDS)
else
    -- Subsequent requests - update without changing TTL
    limit_store:incr(key, 1)
end

-- Set rate limit headers
ngx.header["X-RateLimit-Limit"] = tostring(PROXY_RATE_LIMIT)
ngx.header["X-RateLimit-Remaining"] = tostring(math.max(0, PROXY_RATE_LIMIT - count))

-- Check if over limit
if count > PROXY_RATE_LIMIT then
    ngx.log(ngx.WARN, "rate_limit: exceeded for IP=", client_ip,
            " count=", count, " limit=", PROXY_RATE_LIMIT)
    ngx.header["Retry-After"] = tostring(WINDOW_SECONDS)
    ngx.status = 429
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "rate limit exceeded, try again later", "status": 429}')
    return ngx.exit(429)
end

-- Request is within limits - proceed
