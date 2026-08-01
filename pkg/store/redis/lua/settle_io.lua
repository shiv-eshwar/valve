-- SETTLE split reservation (ITPM + OTPM).
-- KEYS: reservation, rpm, itpm, otpm
-- ARGV: actual_input, actual_output, ttl_sec

local res_key = KEYS[1]
local rpm_key = KEYS[2]
local itpm_key = KEYS[3]
local otpm_key = KEYS[4]
local actual_in = tonumber(ARGV[1])
local actual_out = tonumber(ARGV[2])
local ttl_sec = tonumber(ARGV[3])

if redis.call('EXISTS', res_key) == 0 then
  return redis.error_reply('reservation not found')
end

local fields = redis.call('HMGET', res_key,
  'status', 'mode', 'itpm_reserved', 'otpm_reserved', 'limit_rpm', 'limit_itpm', 'limit_otpm',
  'snap_remaining_rpm', 'snap_remaining_itpm', 'snap_remaining_otpm', 'snap_overshoot_itpm', 'snap_overshoot_otpm')

local status = fields[1]
local mode = fields[2]
if mode ~= 'split' then
  return redis.error_reply('wrong settle mode for reservation')
end
if status == 'settled' then
  return {
    1, '',
    tonumber(fields[8]) or 0,
    tonumber(fields[9]) or 0,
    tonumber(fields[10]) or 0,
    0, 0, 0, 0,
    tonumber(fields[11]) or 0,
    tonumber(fields[12]) or 0,
    tonumber(fields[5]) or 0,
    tonumber(fields[6]) or 0,
    tonumber(fields[7]) or 0
  }
end
if status == 'refunded' then
  return redis.error_reply('reservation already refunded')
end

if actual_in < 0 then actual_in = 0 end
if actual_out < 0 then actual_out = 0 end

local itpm_reserved = tonumber(fields[3])
local otpm_reserved = tonumber(fields[4])
local limit_rpm = tonumber(fields[5])
local limit_itpm = tonumber(fields[6])
local limit_otpm = tonumber(fields[7])

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

local function remaining(tokens)
  return math.floor(tokens)
end

local function reset_ms(tokens, capacity)
  if capacity <= 0 then return now_ms end
  local rate = capacity / 60.0
  local need = capacity - tokens
  if need <= 0 then return now_ms end
  return now_ms + math.ceil((need / rate) * 1000)
end

local function apply_delta(tokens, capacity, reserved, actual)
  local overshoot = 0
  local delta = reserved - actual
  if delta > 0 then
    tokens = math.min(capacity, tokens + delta)
  elseif delta < 0 then
    local need = -delta
    local avail = math.floor(tokens)
    if avail >= need then
      tokens = tokens - need
    else
      overshoot = need - avail
      tokens = tokens - avail
      if tokens < 0 then tokens = 0 end
    end
  end
  return tokens, overshoot
end

local rpm_tokens = refill(rpm_key, limit_rpm)
local itpm_tokens = refill(itpm_key, limit_itpm)
local otpm_tokens = refill(otpm_key, limit_otpm)

local over_in, over_out
itpm_tokens, over_in = apply_delta(itpm_tokens, limit_itpm, itpm_reserved, actual_in)
otpm_tokens, over_out = apply_delta(otpm_tokens, limit_otpm, otpm_reserved, actual_out)

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', itpm_key, 'tokens', itpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', otpm_key, 'tokens', otpm_tokens, 'last_ms', now_ms)

local rem_rpm = remaining(rpm_tokens)
local rem_itpm = remaining(itpm_tokens)
local rem_otpm = remaining(otpm_tokens)

redis.call('HMSET', res_key,
  'status', 'settled',
  'snap_remaining_rpm', rem_rpm,
  'snap_remaining_itpm', rem_itpm,
  'snap_remaining_otpm', rem_otpm,
  'snap_overshoot_itpm', over_in,
  'snap_overshoot_otpm', over_out)
redis.call('EXPIRE', res_key, ttl_sec)

return {1, '', rem_rpm, rem_itpm, rem_otpm, 0,
  reset_ms(rpm_tokens, limit_rpm), reset_ms(itpm_tokens, limit_itpm), reset_ms(otpm_tokens, limit_otpm),
  over_in, over_out, limit_rpm, limit_itpm, limit_otpm}
