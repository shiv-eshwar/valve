-- Return unused lease credits to RPM + ITPM + OTPM.
-- KEYS: rpm, itpm, otpm
-- ARGV: limit_rpm, limit_itpm, limit_otpm, add_rpm, add_itpm, add_otpm

local rpm_key = KEYS[1]
local itpm_key = KEYS[2]
local otpm_key = KEYS[3]

local limit_rpm = tonumber(ARGV[1])
local limit_itpm = tonumber(ARGV[2])
local limit_otpm = tonumber(ARGV[3])
local add_rpm = tonumber(ARGV[4])
local add_itpm = tonumber(ARGV[5])
local add_otpm = tonumber(ARGV[6])

if add_rpm < 0 then add_rpm = 0 end
if add_itpm < 0 then add_itpm = 0 end
if add_otpm < 0 then add_otpm = 0 end
if add_rpm == 0 and add_itpm == 0 and add_otpm == 0 then
  return 1
end

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

rpm_tokens = math.min(limit_rpm, rpm_tokens + add_rpm)
itpm_tokens = math.min(limit_itpm, itpm_tokens + add_itpm)
otpm_tokens = math.min(limit_otpm, otpm_tokens + add_otpm)

redis.call('HMSET', rpm_key, 'tokens', rpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', itpm_key, 'tokens', itpm_tokens, 'last_ms', now_ms)
redis.call('HMSET', otpm_key, 'tokens', otpm_tokens, 'last_ms', now_ms)
return 1
