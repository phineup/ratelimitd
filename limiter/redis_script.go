package limiter

// luaScript evaluates all rules of a policy for one key atomically inside
// Redis. It mirrors algorithms.go formula for formula (including EPS) so the
// memory and Redis stores give identical decisions; redis_test.go checks this.
//
// KEYS[i]  state key of rule i (all share one {hash tag} so this works on
//
//	Redis Cluster too)
//
// ARGV[1]  consume ("1" = count the request, "0" = peek)
// ARGV[2]  cost
// ARGV[3]  unique request id (sliding_log member names)
// ARGV[4]  unix ms override; 0 (always, in production) = use Redis TIME
// ARGV[5]  number of rules n
// then per rule: algorithm, limit, window_ms, burst, anchor_first ("1"/"0")
//
// Time comes from Redis (TIME), so several limiter instances with skewed
// clocks still agree. The override exists only so tests can drive both stores
// with one fake clock. Returns: {allowed, then per rule: allowed, remBefore,
// remAfter, retryMs, resetMs}.
const luaScript = `
local EPS = 1e-9
local consume = ARGV[1] == '1'
local cost = tonumber(ARGV[2])
local reqid = ARGV[3]
local now = tonumber(ARGV[4])
if now == 0 then
  local t = redis.call('TIME')
  now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
end
local n = tonumber(ARGV[5])

local outs = {}
local commits = {}
local all = true

for i = 1, n do
  local b = 5 + (i - 1) * 5
  local algo = ARGV[b + 1]
  local limit = tonumber(ARGV[b + 2])
  local w = tonumber(ARGV[b + 3])
  local burst = tonumber(ARGV[b + 4])
  local anchor_first = ARGV[b + 5] == '1'
  local key = KEYS[i]

  local allowed, rb, ra, retry, reset = false, 0, 0, 0, 0
  local commit = nil

  if algo == 'fixed_window' then
    local st = redis.call('HMGET', key, 'start', 'count')
    local start = tonumber(st[1]) or 0
    local count = tonumber(st[2]) or 0
    if anchor_first then
      if start == 0 or now >= start + w then start = now; count = 0 end
    else
      local cs = now - (now % w)
      if start ~= cs then start = cs; count = 0 end
    end
    rb = math.max(0, limit - count)
    local nc = count + cost
    if nc <= limit then
      allowed = true
      ra = limit - nc
      commit = function()
        redis.call('HSET', key, 'start', start, 'count', nc)
        redis.call('PEXPIRE', key, start + w - now + 1000)
      end
    else
      ra = rb
      retry = start + w - now
    end
    reset = start + w - now

  elseif algo == 'sliding_window' then
    local st = redis.call('HMGET', key, 'start', 'count', 'prev')
    local start = tonumber(st[1]) or 0
    local count = tonumber(st[2]) or 0
    local prev = tonumber(st[3]) or 0
    local cs = now - (now % w)
    if start == 0 then start = cs end
    if cs ~= start then
      if cs - start >= 2 * w then prev = 0; count = 0 else prev = count; count = 0 end
      start = cs
    end
    local elapsed = now - start
    local est = prev * (1 - elapsed / w) + count
    rb = math.max(0, math.floor(limit - est + EPS))
    if est + cost <= limit + EPS then
      allowed = true
      ra = math.max(0, math.floor(limit - est - cost + EPS))
      count = count + cost
      commit = function()
        redis.call('HSET', key, 'start', start, 'count', count, 'prev', prev)
        redis.call('PEXPIRE', key, 2 * w + 1000)
      end
    else
      ra = rb
      if prev > 0 and limit - count - cost >= 0 then
        local x = w * (1 - (limit - count - cost) / prev)
        retry = math.ceil(x) - elapsed
      else
        local rem = limit - cost
        local x2 = 0
        if count > 0 then x2 = w * (1 - rem / count) end
        if x2 < 0 then x2 = 0 end
        retry = (w - elapsed) + math.ceil(x2)
      end
      if retry < 1 then retry = 1 end
    end
    if count > 0 then reset = start + 2 * w - now
    elseif prev > 0 then reset = start + w - now end

  elseif algo == 'sliding_log' then
    redis.call('ZREMRANGEBYSCORE', key, '-inf', now - w)
    local count = redis.call('ZCARD', key)
    rb = math.max(0, limit - count)
    if count + cost <= limit then
      allowed = true
      ra = limit - count - cost
      reset = w
      commit = function()
        for j = 1, cost do
          redis.call('ZADD', key, now, reqid .. '-' .. i .. '-' .. j)
        end
        redis.call('PEXPIRE', key, w + 1000)
      end
    else
      ra = rb
      local idx = count + cost - limit - 1
      local e = redis.call('ZRANGE', key, idx, idx, 'WITHSCORES')
      retry = tonumber(e[2]) + w - now
      local l = redis.call('ZRANGE', key, -1, -1, 'WITHSCORES')
      reset = tonumber(l[2]) + w - now
    end

  else -- token_bucket
    local capf = limit
    if burst > 0 then capf = burst end
    local rate = limit / w
    local st = redis.call('HMGET', key, 'tokens', 'ts')
    local tokens = tonumber(st[1])
    local ts = tonumber(st[2])
    if tokens == nil or ts == nil or ts == 0 then
      tokens = capf
    else
      local elapsed = now - ts
      if elapsed < 0 then elapsed = 0 end
      tokens = math.min(capf, tokens + elapsed * rate)
    end
    rb = math.floor(tokens + EPS)
    local after = tokens
    if tokens + EPS >= cost then
      allowed = true
      after = tokens - cost
      if after < 0 then after = 0 end
      ra = math.floor(after + EPS)
      commit = function()
        redis.call('HSET', key, 'tokens', after, 'ts', now)
        redis.call('PEXPIRE', key, math.ceil(capf / rate) + 1000)
      end
    else
      ra = rb
      retry = math.ceil((cost - tokens) / rate)
    end
    reset = math.ceil((capf - after) / rate)
  end

  if not allowed then all = false end
  outs[i] = {allowed and 1 or 0, rb, ra, retry, reset}
  if commit then commits[#commits + 1] = commit end
end

if consume and all then
  for _, c in ipairs(commits) do c() end
end

local res = {all and 1 or 0}
for i = 1, n do
  for j = 1, 5 do res[#res + 1] = outs[i][j] end
end
return res
`
