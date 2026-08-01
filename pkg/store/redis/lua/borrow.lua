-- Borrow a lease chunk (all-or-nothing across RPM + TPM). No reservation.
-- KEYS[1] = rpm hash, KEYS[2] = tpm hash
-- ARGV: limit_rpm, limit_tpm, min_rpm, min_tpm, chunk_rpm, chunk_tpm
-- Returns: ok, limit_type, got_rpm, got_tpm, rem_rpm, rem_tpm, retry_after_ms

local rpm_key = KEYS[1]
local tpm_key = KEYS[2]

local limit_rpm = tonumber(ARGV[1])
local limit_tpm = tonumber(ARGV[2])
local min_rpm = tonumber(ARGV[3])
local min_tpm = tonumber(ARGV[4])
local chunk_rpm = tonumber(ARGV[5])
local chunk_tpm = tonumber(ARGV[6])

if min_rpm < 0 then min_rpm = 0 end
if min_tpm < 0 then min_tpm = 0 end
if chunk_rpm < 0 then chunk_rpm = 0 end
if chunk_tpm < 0 then chunk_tpm = 0 end

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

local rpm_tokens = refill(rpm_key, limit_rpm)
local tpm_tokens = refill(tpm_key, limit_tpm)

local avail_rpm = math.floor(rpm_tokens)
local avail_tpm = math.floor(tpm_tokens)

if avail_rpm < min_rpm then
  local ra = retry_after_ms(rpm_tokens, limit_rpm, min_rpm)
  return {0, 'requests', 0, 0, avail_rpm, avail_tpm, ra}
end

if avail_tpm < min_tpm then
  local ra = retry_after_ms(tpm_tokens, limit_tpm, min_tpm)
  return {0, 'tokens', 0, 0, avail_rpm, avail_tpm, ra}
end

local want_rpm = math.max(min_rpm, chunk_rpm)
local want_tpm = math.max(min_tpm, chunk_tpm)
local got_rpm = math.min(avail_rpm, want_rpm)
local got_tpm = math.min(avail_tpm, want_tpm)

rpm_tokens = rpm_tokens - got_rpm
tpm_tokens = tpm_tokens - got_tpm

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', tpm_key, 'tokens', tpm_tokens, 'last_ms', now_ms)

return {1, '', got_rpm, got_tpm, remaining(rpm_tokens), remaining(tpm_tokens), 0}
