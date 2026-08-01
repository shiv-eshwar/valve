-- Borrow lease chunks from RPM + ITPM + OTPM.
-- KEYS: rpm, itpm, otpm
-- ARGV: limit_rpm, limit_itpm, limit_otpm, min_rpm, min_itpm, min_otpm, chunk_rpm, chunk_itpm, chunk_otpm

local rpm_key = KEYS[1]
local itpm_key = KEYS[2]
local otpm_key = KEYS[3]

local limit_rpm = tonumber(ARGV[1])
local limit_itpm = tonumber(ARGV[2])
local limit_otpm = tonumber(ARGV[3])
local min_rpm = tonumber(ARGV[4])
local min_itpm = tonumber(ARGV[5])
local min_otpm = tonumber(ARGV[6])
local chunk_rpm = tonumber(ARGV[7])
local chunk_itpm = tonumber(ARGV[8])
local chunk_otpm = tonumber(ARGV[9])

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

local function retry_after_ms(tokens, capacity, need)
  if capacity <= 0 then return 3600000 end
  local rate = capacity / 60.0
  local deficit = need - tokens
  if deficit <= 0 then return 0 end
  return math.ceil(deficit / rate) * 1000
end

local function want_got(avail, minv, chunk)
  local want = minv
  if chunk > want then want = chunk end
  if avail < want then return avail end
  return want
end

if min_rpm < 0 then min_rpm = 0 end
if min_itpm < 0 then min_itpm = 0 end
if min_otpm < 0 then min_otpm = 0 end
if chunk_rpm < 0 then chunk_rpm = 0 end
if chunk_itpm < 0 then chunk_itpm = 0 end
if chunk_otpm < 0 then chunk_otpm = 0 end

local rpm_tokens = refill(rpm_key, limit_rpm)
local itpm_tokens = refill(itpm_key, limit_itpm)
local otpm_tokens = refill(otpm_key, limit_otpm)
local avail_rpm = remaining(rpm_tokens)
local avail_itpm = remaining(itpm_tokens)
local avail_otpm = remaining(otpm_tokens)

-- return: allowed, limit_type, got_rpm, got_itpm, got_otpm, rem_rpm, rem_itpm, rem_otpm, retry_ms
if avail_rpm < min_rpm then
  return {0, 'requests', 0, 0, 0, avail_rpm, avail_itpm, avail_otpm, retry_after_ms(rpm_tokens, limit_rpm, min_rpm)}
end
if avail_itpm < min_itpm then
  return {0, 'input_tokens', 0, 0, 0, avail_rpm, avail_itpm, avail_otpm, retry_after_ms(itpm_tokens, limit_itpm, min_itpm)}
end
if avail_otpm < min_otpm then
  return {0, 'output_tokens', 0, 0, 0, avail_rpm, avail_itpm, avail_otpm, retry_after_ms(otpm_tokens, limit_otpm, min_otpm)}
end

local got_rpm = want_got(avail_rpm, min_rpm, chunk_rpm)
local got_itpm = want_got(avail_itpm, min_itpm, chunk_itpm)
local got_otpm = want_got(avail_otpm, min_otpm, chunk_otpm)

rpm_tokens = rpm_tokens - got_rpm
itpm_tokens = itpm_tokens - got_itpm
otpm_tokens = otpm_tokens - got_otpm

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', itpm_key, 'tokens', itpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', otpm_key, 'tokens', otpm_tokens, 'last_ms', now_ms)

return {1, '', got_rpm, got_itpm, got_otpm, remaining(rpm_tokens), remaining(itpm_tokens), remaining(otpm_tokens), 0}
