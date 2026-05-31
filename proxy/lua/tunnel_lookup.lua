-- tunnel_lookup.lua
-- OpenResty access phase script for:
--   1. Per-IP rate limiting (using lua_shared_dict, no Redis round-trip)
--   2. Tunnel subdomain validation (Redis lookup)
--   3. IP access control (allowed_ips enforcement)

local redis = require "resty.redis"
local cjson = require "cjson.safe"

-- ============================================================
-- RATE LIMITING (in-process, no Redis needed)
-- ============================================================
local limit_store = ngx.shared.rate_limit_store
local PROXY_RATE_LIMIT = PROXY_RATE_LIMIT_RPM or 600  -- from env, default 600 req/min
local WINDOW_SECONDS = 60

local client_ip = ngx.var.remote_addr
local rate_key = "proxy:" .. client_ip

local count, err = limit_store:get(rate_key)
if count == nil then
    count = 0
end

count = count + 1

if count == 1 then
    limit_store:set(rate_key, count, WINDOW_SECONDS)
else
    limit_store:incr(rate_key, 1)
end

ngx.header["X-RateLimit-Limit"] = tostring(PROXY_RATE_LIMIT)
ngx.header["X-RateLimit-Remaining"] = tostring(math.max(0, PROXY_RATE_LIMIT - count))

if count > PROXY_RATE_LIMIT then
    ngx.log(ngx.WARN, "rate_limit: exceeded for IP=", client_ip,
            " count=", count, " limit=", PROXY_RATE_LIMIT)
    ngx.header["Retry-After"] = tostring(WINDOW_SECONDS)
    ngx.status = 429
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "rate limit exceeded, try again later", "status": 429}')
    return ngx.exit(429)
end

-- ============================================================
-- TUNNEL LOOKUP (Redis)
-- ============================================================
local REDIS_HOST = "127.0.0.1"
local REDIS_PORT = 6379
local REDIS_TIMEOUT = 1000 -- ms
local REDIS_POOL_SIZE = 100
local REDIS_KEEPALIVE = 10000 -- ms

local REDIS_PASSWORD = os.getenv("REDIS_PASSWORD") or ""

-- Extract subdomain from nginx variable (captured by server_name regex)
local subdomain = ngx.var.subdomain

if not subdomain or subdomain == "" then
    ngx.log(ngx.WARN, "tunnel_lookup: empty subdomain")
    ngx.status = 404
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "tunnel not found", "status": 404}')
    return ngx.exit(404)
end

-- Strict format validation
if not ngx.re.match(subdomain, "^[a-z0-9][a-z0-9\\-]{1,38}[a-z0-9]$", "jo") then
    ngx.log(ngx.WARN, "tunnel_lookup: invalid subdomain format: ", subdomain)
    ngx.status = 404
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "tunnel not found", "status": 404}')
    return ngx.exit(404)
end

-- Connect to Redis
local red = redis:new()
red:set_timeout(REDIS_TIMEOUT)

local ok, conn_err = red:connect(REDIS_HOST, REDIS_PORT)
if not ok then
    ngx.log(ngx.ERR, "tunnel_lookup: redis connect failed: ", conn_err)
    ngx.status = 502
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "service temporarily unavailable", "status": 502}')
    return ngx.exit(502)
end

-- Authenticate if password is set
if REDIS_PASSWORD ~= "" then
    local auth_ok, auth_err = red:auth(REDIS_PASSWORD)
    if not auth_ok then
        ngx.log(ngx.ERR, "tunnel_lookup: redis auth failed: ", auth_err)
        ngx.status = 502
        ngx.header["Content-Type"] = "application/json"
        ngx.say('{"error": "service temporarily unavailable", "status": 502}')
        return ngx.exit(502)
    end
end

-- Look up tunnel
local tunnel_key = "tunnel:" .. subdomain
local tunnel_data, get_err = red:get(tunnel_key)

if not tunnel_data then
    ngx.log(ngx.ERR, "tunnel_lookup: redis get failed: ", get_err)
    red:set_keepalive(REDIS_KEEPALIVE, REDIS_POOL_SIZE)
    ngx.status = 502
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "service temporarily unavailable", "status": 502}')
    return ngx.exit(502)
end

if tunnel_data == ngx.null then
    ngx.log(ngx.INFO, "tunnel_lookup: not found: ", subdomain,
            " client_ip=", client_ip)
    red:set_keepalive(REDIS_KEEPALIVE, REDIS_POOL_SIZE)
    ngx.status = 404
    ngx.header["Content-Type"] = "application/json"
    ngx.say('{"error": "tunnel not found or expired", "status": 404}')
    return ngx.exit(404)
end

-- Return connection to pool (done with Redis)
red:set_keepalive(REDIS_KEEPALIVE, REDIS_POOL_SIZE)

-- ============================================================
-- IP ACCESS CONTROL
-- ============================================================
local tunnel = cjson.decode(tunnel_data)
if tunnel then
    -- Check allowed_ips
    local allowed_ips = tunnel.allowed_ips
    if allowed_ips and type(allowed_ips) == "table" then
        local is_allowed = false

        for _, allowed_ip in ipairs(allowed_ips) do
            if allowed_ip == "any" or allowed_ip == "*" then
                is_allowed = true
                break
            end
            if allowed_ip == client_ip then
                is_allowed = true
                break
            end
        end

        if not is_allowed then
            ngx.log(ngx.WARN, "tunnel_lookup: IP denied",
                    " subdomain=", subdomain,
                    " client_ip=", client_ip,
                    " allowed_ips=", table.concat(allowed_ips, ","))
            ngx.status = 403
            ngx.header["Content-Type"] = "application/json"
            ngx.say('{"error": "access denied: your IP is not allowed", "status": 403}')
            return ngx.exit(403)
        end
    end

    -- Log successful access
    ngx.log(ngx.INFO, "tunnel_lookup: access granted",
            " subdomain=", subdomain,
            " client_ip=", client_ip,
            " duration=", (tunnel.duration or "unknown"),
            " expires_at=", (tunnel.expires_at or "unlimited"))
end

-- Request proceeds to proxy_pass
