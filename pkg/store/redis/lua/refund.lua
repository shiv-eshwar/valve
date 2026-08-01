-- REFUND a pending reservation (restore RPM + TPM).
-- KEYS[1] = reservation hash, KEYS[2] = rpm hash, KEYS[3] = tpm hash
-- ARGV: ttl_sec

local res_key = KEYS[1]
local rpm_key = KEYS[2]
local tpm_key = KEYS[3]
local ttl_sec = tonumber(ARGV[1])

if redis.call('EXISTS', res_key) == 0 then
  return redis.error_reply('reservation not found')
end

local fields = redis.call('HMGET', res_key, 'status', 'rpm_cost', 'tpm_reserved', 'limit_rpm', 'limit_tpm')
local status = fields[1]
if status == 'refunded' then
  return {1}
end
if status == 'settled' then
  return redis.error_reply('reservation already settled')
end

local rpm_cost = tonumber(fields[2])
local tpm_reserved = tonumber(fields[3])
local limit_rpm = tonumber(fields[4])
local limit_tpm = tonumber(fields[5])

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
  return tokens, last_ms
end

local rpm_tokens = refill(rpm_key, limit_rpm)
local tpm_tokens = refill(tpm_key, limit_tpm)

rpm_tokens = math.min(limit_rpm, rpm_tokens + rpm_cost)
tpm_tokens = math.min(limit_tpm, tpm_tokens + tpm_reserved)

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', tpm_key, 'tokens', tpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', res_key, 'status', 'refunded')
redis.call('EXPIRE', res_key, ttl_sec)

return {1}
