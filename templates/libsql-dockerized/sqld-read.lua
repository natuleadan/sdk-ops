wrk.method = "POST"
wrk.body = '{"statements":["SELECT 1 AS test"]}'
wrk.headers["Content-Type"] = "application/json"
