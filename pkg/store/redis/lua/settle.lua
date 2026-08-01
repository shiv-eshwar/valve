-- SETTLE reservation against actual token usage.
-- KEYS[1] = reservation hash, KEYS[2] = rpm hash, KEYS[3] = tpm hash
-- ARGV: actual_tokens, ttl_sec

local res_key = KEYS[1]
local rpm_key = KEYS[2]
local tpm_key = KEYS[3]
local actual = tonumber(ARGV[1])
local ttl_sec = tonumber(ARGV[2])

if redis.call('EXISTS', res_key) == 0 then
  return redis.error_reply('reservation not found')
end

local fields = redis.call('HMGET', res_key,
  'status', 'subject', 'model', 'rpm_cost', 'tpm_reserved', 'limit_rpm', 'limit_tpm',
  'snap_allowed', 'snap_remaining_rpm', 'snap_remaining_tpm', 'snap_overshoot')

local status = fields[1]
if status == 'settled' then
  return {
    1,
    '',
    tonumber(fields[9]) or 0,
    tonumber(fields[10]) or 0,
    0,
    0,
    0,
    tonumber(fields[11]) or 0,
    tonumber(fields[6]) or 0,
    tonumber(fields[7]) or 0
  }
end
if status == 'refunded' then
  return redis.error_reply('reservation already refunded')
end

if actual < 0 then actual = 0 end

local tpm_reserved = tonumber(fields[5])
local limit_rpm = tonumber(fields[6])
local limit_tpm = tonumber(fields[7])

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

local rpm_tokens = refill(rpm_key, limit_rpm)
local tpm_tokens = refill(tpm_key, limit_tpm)

local overshoot = 0
local delta = tpm_reserved - actual
if delta > 0 then
  tpm_tokens = math.min(limit_tpm, tpm_tokens + delta)
elseif delta < 0 then
  local need = -delta
  local avail = math.floor(tpm_tokens)
  if avail >= need then
    tpm_tokens = tpm_tokens - need
  else
    overshoot = need - avail
    tpm_tokens = tpm_tokens - avail
    if tpm_tokens < 0 then tpm_tokens = 0 end
  end
end

redis.call('HMSET', tpm_key, 'tokens', tpm_tokens, 'last_ms', now_ms)
-- rpm unchanged except refill timestamp consistency
redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)

local rem_rpm = remaining(rpm_tokens)
local rem_tpm = remaining(tpm_tokens)

redis.call('HMSET', res_key,
  'status', 'settled',
  'snap_allowed', 1,
  'snap_remaining_rpm', rem_rpm,
  'snap_remaining_tpm', rem_tpm,
  'snap_overshoot', overshoot)
redis.call('EXPIRE', res_key, ttl_sec)

return {1, '', rem_rpm, rem_tpm, 0, reset_ms(rpm_tokens, limit_rpm), reset_ms(tpm_tokens, limit_tpm), overshoot, limit_rpm, limit_tpm}
