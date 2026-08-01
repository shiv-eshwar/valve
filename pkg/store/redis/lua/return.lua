-- Return unused lease credits to shared budgets (capped at capacity).
-- KEYS[1] = rpm hash, KEYS[2] = tpm hash
-- ARGV: limit_rpm, limit_tpm, rpm, tpm

local rpm_key = KEYS[1]
local tpm_key = KEYS[2]

local limit_rpm = tonumber(ARGV[1])
local limit_tpm = tonumber(ARGV[2])
local add_rpm = tonumber(ARGV[3])
local add_tpm = tonumber(ARGV[4])

if add_rpm < 0 then add_rpm = 0 end
if add_tpm < 0 then add_tpm = 0 end

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

rpm_tokens = math.min(limit_rpm, rpm_tokens + add_rpm)
tpm_tokens = math.min(limit_tpm, tpm_tokens + add_tpm)

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', tpm_key, 'tokens', tpm_tokens, 'last_ms', now_ms)

return {1, math.floor(rpm_tokens), math.floor(tpm_tokens)}
