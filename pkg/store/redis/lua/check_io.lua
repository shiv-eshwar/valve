-- Triple token-bucket CHECK: RPM + ITPM + OTPM (all-or-nothing).
-- KEYS: rpm, itpm, otpm, reservation
-- ARGV: limit_rpm, limit_itpm, limit_otpm, cost_rpm, cost_itpm, cost_otpm, ttl_sec, subject, model

local rpm_key = KEYS[1]
local itpm_key = KEYS[2]
local otpm_key = KEYS[3]
local res_key = KEYS[4]

local limit_rpm = tonumber(ARGV[1])
local limit_itpm = tonumber(ARGV[2])
local limit_otpm = tonumber(ARGV[3])
local cost_rpm = tonumber(ARGV[4])
local cost_itpm = tonumber(ARGV[5])
local cost_otpm = tonumber(ARGV[6])
local ttl_sec = tonumber(ARGV[7])
local subject = ARGV[8]
local model = ARGV[9]

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
if cost_itpm < 0 then cost_itpm = 0 end
if cost_otpm < 0 then cost_otpm = 0 end

local rpm_tokens = refill(rpm_key, limit_rpm)
local itpm_tokens = refill(itpm_key, limit_itpm)
local otpm_tokens = refill(otpm_key, limit_otpm)

-- return: allowed, limit_type, rem_rpm, rem_itpm, rem_otpm, retry_ms, reset_rpm, reset_itpm, reset_otpm
if rpm_tokens < cost_rpm or cost_rpm > limit_rpm then
  local ra = retry_after_ms(rpm_tokens, limit_rpm, cost_rpm)
  return {0, 'requests', remaining(rpm_tokens), remaining(itpm_tokens), remaining(otpm_tokens), ra,
    reset_ms(rpm_tokens, limit_rpm), reset_ms(itpm_tokens, limit_itpm), reset_ms(otpm_tokens, limit_otpm)}
end
if itpm_tokens < cost_itpm or cost_itpm > limit_itpm then
  local ra = retry_after_ms(itpm_tokens, limit_itpm, cost_itpm)
  return {0, 'input_tokens', remaining(rpm_tokens), remaining(itpm_tokens), remaining(otpm_tokens), ra,
    reset_ms(rpm_tokens, limit_rpm), reset_ms(itpm_tokens, limit_itpm), reset_ms(otpm_tokens, limit_otpm)}
end
if otpm_tokens < cost_otpm or cost_otpm > limit_otpm then
  local ra = retry_after_ms(otpm_tokens, limit_otpm, cost_otpm)
  return {0, 'output_tokens', remaining(rpm_tokens), remaining(itpm_tokens), remaining(otpm_tokens), ra,
    reset_ms(rpm_tokens, limit_rpm), reset_ms(itpm_tokens, limit_itpm), reset_ms(otpm_tokens, limit_otpm)}
end

rpm_tokens = rpm_tokens - cost_rpm
itpm_tokens = itpm_tokens - cost_itpm
otpm_tokens = otpm_tokens - cost_otpm

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', itpm_key, 'tokens', itpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', otpm_key, 'tokens', otpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', res_key,
  'subject', subject,
  'model', model,
  'mode', 'split',
  'rpm_cost', cost_rpm,
  'itpm_reserved', cost_itpm,
  'otpm_reserved', cost_otpm,
  'limit_rpm', limit_rpm,
  'limit_itpm', limit_itpm,
  'limit_otpm', limit_otpm,
  'status', 'pending',
  'created_ms', now_ms)
redis.call('EXPIRE', res_key, ttl_sec)

return {1, '', remaining(rpm_tokens), remaining(itpm_tokens), remaining(otpm_tokens), 0,
  reset_ms(rpm_tokens, limit_rpm), reset_ms(itpm_tokens, limit_itpm), reset_ms(otpm_tokens, limit_otpm)}
