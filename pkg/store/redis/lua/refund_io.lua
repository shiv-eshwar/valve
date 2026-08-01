-- REFUND split reservation.
-- KEYS: reservation, rpm, itpm, otpm
-- ARGV: ttl_sec

local res_key = KEYS[1]
local rpm_key = KEYS[2]
local itpm_key = KEYS[3]
local otpm_key = KEYS[4]
local ttl_sec = tonumber(ARGV[1])

if redis.call('EXISTS', res_key) == 0 then
  return redis.error_reply('reservation not found')
end

local fields = redis.call('HMGET', res_key,
  'status', 'mode', 'rpm_cost', 'itpm_reserved', 'otpm_reserved', 'limit_rpm', 'limit_itpm', 'limit_otpm')

local status = fields[1]
if status == 'refunded' then
  return 1
end
if status == 'settled' then
  return redis.error_reply('reservation already settled')
end
if fields[2] ~= 'split' then
  return redis.error_reply('wrong settle mode for reservation')
end

local rpm_cost = tonumber(fields[3]) or 0
local itpm_reserved = tonumber(fields[4]) or 0
local otpm_reserved = tonumber(fields[5]) or 0
local limit_rpm = tonumber(fields[6])
local limit_itpm = tonumber(fields[7])
local limit_otpm = tonumber(fields[8])

local t = redis.call('TIME')
local now_ms = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)

local function refill(key, capacity)
  local data = redis.call('HMGET', key, 'tokens', 'last_ms')
  local tokens = tonumber(data[1])
  local last_ms = tonumber(data[2])
  if tokens == nil or last_ms == nil then
    tokens = capacity
    last_ms = now_ms
  else
    if now_ms > last_ms and capacity > 0 then
      local elapsed = (now_ms - last_ms) / 1000.0
      local rate = capacity / 60.0
      tokens = math.min(capacity, tokens + elapsed * rate)
      last_ms = now_ms
    end
  end
  return tokens
end

local rpm_tokens = refill(rpm_key, limit_rpm)
local itpm_tokens = refill(itpm_key, limit_itpm)
local otpm_tokens = refill(otpm_key, limit_otpm)

rpm_tokens = math.min(limit_rpm, rpm_tokens + rpm_cost)
itpm_tokens = math.min(limit_itpm, itpm_tokens + itpm_reserved)
otpm_tokens = math.min(limit_otpm, otpm_tokens + otpm_reserved)

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', itpm_key, 'tokens', itpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', otpm_key, 'tokens', otpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', res_key, 'status', 'refunded')
redis.call('EXPIRE', res_key, ttl_sec)
return 1
