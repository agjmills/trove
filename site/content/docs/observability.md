---
title: Observability
weight: 7
---

Trove exposes health checks, Prometheus metrics, and structured logs.

## Health check

`GET /health` returns a JSON response with the overall status of the server and each subsystem:

```json
{
  "status": "healthy",
  "version": "1.0.0 (commit: abc123)",
  "checks": {
    "database": {"status": "healthy", "latency": "2.1ms"},
    "storage":  {"status": "healthy", "latency": "0.5ms"}
  },
  "uptime": "2h15m30s"
}
```

The response is `200 OK` when healthy. If any check fails the overall `status` becomes `unhealthy`.

Use this as a Docker / Kubernetes liveness probe:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
```

## Prometheus metrics

`GET /metrics` returns Prometheus-format metrics.

| Metric | Type | Description |
|--------|------|-------------|
| `trove_http_requests_total` | Counter | HTTP requests by method, path, and status code |
| `trove_http_request_duration_seconds` | Histogram | Request latency |
| `trove_http_requests_in_flight` | Gauge | Current concurrent requests |
| `trove_storage_usage_bytes` | Gauge | Storage used per user |
| `trove_files_total` | Counter | File upload count |
| `trove_login_attempts_total` | Counter | Authentication attempts |

> The metrics endpoint is unauthenticated. In production, restrict access with your reverse proxy or firewall.

Example Nginx snippet to allow metrics only from an internal network:

```nginx
location /metrics {
    allow 10.0.0.0/8;
    deny all;
    proxy_pass http://localhost:8080;
}
```

## Structured logging

In `ENV=production`, Trove writes JSON logs to stdout:

```json
{"time":"2025-11-24T10:30:00Z","level":"INFO","msg":"http request","method":"POST","path":"/upload","status":200,"duration_ms":145}
```

In `ENV=development`, logs use a human-readable text format for easier local debugging.

Log lines include:

- `method`, `path`, `status`, `duration_ms` for every HTTP request
- `user_id` where relevant
- Error messages and stack details when something goes wrong
