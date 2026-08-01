-- Dual token-bucket CHECK (all-or-nothing).
-- KEYS[1] = rpm hash, KEYS[2] = tpm hash, KEYS[3] = reservation hash
-- ARGV: limit_rpm, limit_tpm, cost_rpm, cost_tpm, ttl_sec, subject, model

local rpm_key = KEYS[1]
local tpm_key = KEYS[2]
local res_key = KEYS[3]

local limit_rpm = tonumber(ARGV[1])
local limit_tpm = tonumber(ARGV[2])
local cost_rpm = tonumber(ARGV[3])
local cost_tpm = tonumber(ARGV[4])
local ttl_sec = tonumber(ARGV[5])
local subject = ARGV[6]
local model = ARGV[7]

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

local function remaining(tokens)
  return math.floor(tokens)
end

local function retry_after_ms(tokens, capacity, need)
  if capacity <= 0 then
    return 3600000
  end
  local rate = capacity / 60.0
  local deficit = need - tokens
  if deficit <= 0 then
    return 0
  end
  return math.ceil(deficit / rate) * 1000
end

local function reset_ms(tokens, capacity)
  if capacity <= 0 then
    return now_ms
  end
  local rate = capacity / 60.0
  local need = capacity - tokens
  if need <= 0 then
    return now_ms
  end
  return now_ms + math.ceil((need / rate) * 1000)
end

if cost_rpm < 0 then cost_rpm = 0 end
if cost_tpm < 0 then cost_tpm = 0 end

local rpm_tokens, rpm_last = refill(rpm_key, limit_rpm)
local tpm_tokens, tpm_last = refill(tpm_key, limit_tpm)

-- deny requests
if rpm_tokens < cost_rpm or cost_rpm > limit_rpm then
  local ra = retry_after_ms(rpm_tokens, limit_rpm, cost_rpm)
  return {0, 'requests', remaining(rpm_tokens), remaining(tpm_tokens), ra, reset_ms(rpm_tokens, limit_rpm), reset_ms(tpm_tokens, limit_tpm), 0}
end

-- deny tokens (no RPM write)
if tpm_tokens < cost_tpm or cost_tpm > limit_tpm then
  local ra = retry_after_ms(tpm_tokens, limit_tpm, cost_tpm)
  return {0, 'tokens', remaining(rpm_tokens), remaining(tpm_tokens), ra, reset_ms(rpm_tokens, limit_rpm), reset_ms(tpm_tokens, limit_tpm), 0}
end

rpm_tokens = rpm_tokens - cost_rpm
tpm_tokens = tpm_tokens - cost_tpm

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', tpm_key, 'tokens', tpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', res_key,
  'subject', subject,
  'model', model,
  'rpm_cost', cost_rpm,
  'tpm_reserved', cost_tpm,
  'limit_rpm', limit_rpm,
  'limit_tpm', limit_tpm,
  'status', 'pending',
  'created_ms', now_ms)
redis.call('EXPIRE', res_key, ttl_sec)

return {1, '', remaining(rpm_tokens), remaining(tpm_tokens), 0, reset_ms(rpm_tokens, limit_rpm), reset_ms(tpm_tokens, limit_tpm), 0}
