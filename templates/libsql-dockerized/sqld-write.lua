wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"
request = function()
  local id = math.random(1, 999999999)
  wrk.body = '{"statements":["INSERT INTO bench_kv(id, val) VALUES(' .. id .. ", 'payload_" .. id .. "')"]}'
  return wrk.format()
end
