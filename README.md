# Trove

Self-hosted file storage. Your personal Google Drive alternative.

**Simple. Fast. Privacy-focused.**

## Features

- 📤 Upload, organize, and manage files with drag-and-drop
- 📦 Streaming uploads for large files (multi-GB support)
- 👥 Multi-user support with authentication and per-user quotas
- 🎨 Modern UI with dark mode
- 🔒 Secure by default (CSRF protection, bcrypt, rate limiting)
- 🐳 Easy Docker deployment
- 🗄️ PostgreSQL or SQLite

## Quick Start

**Prerequisites:** Docker and Docker Compose

```bash
git clone https://github.com/agjmills/trove.git
cd trove
cp .env.example .env
make setup
```

Trove is now running at `http://localhost:8080`

**Using pre-built images:**

```bash
docker pull ghcr.io/agjmills/trove:latest
```

Multi-arch images available for `linux/amd64` and `linux/arm64`.

## Development

```bash
make dev      # Start with hot-reload
make test     # Run tests
make shell    # Container shell
make psql     # Database console
```

## Configuration

Edit `.env` for your setup:

```bash
TROVE_PORT=8080                    # Host port
DB_TYPE=postgres                   # or sqlite
SESSION_SECRET=random_secret_here  # Required
DEFAULT_USER_QUOTA=10G             # Per-user limit
MAX_UPLOAD_SIZE=500M               # Max file size
ENV=development                    # or production
```

See `.env.example` for all options.

## Production

```bash
# 1. Setup
cp .env.example .env                          # Configure
cp docker-compose.example.yml docker-compose.prod.yml  # Customize

# 2. Deploy
docker compose -f docker-compose.prod.yml up -d

# 3. Monitor
docker compose -f docker-compose.prod.yml logs -f
```

**Recommended:** Run behind a reverse proxy (Caddy/Nginx) for HTTPS.

**Security Best Practices:**
- Use a strong `SESSION_SECRET` (generate with `openssl rand -base64 32`)
- Restrict `/metrics` endpoint access via firewall or reverse proxy auth
- **Enable HTTPS in production:** Set `ENV=production` to enable strict CSRF origin validation
  - For HTTP-only environments (homelab), use `ENV=development` (CSRF token validation still active)
  - **Behind reverse proxy with TLS termination:** Ensure proxy forwards `X-Forwarded-Proto` header
    - Traefik: automatic with `--providers.docker.exposedByDefault=false`
    - Nginx: `proxy_set_header X-Forwarded-Proto $scheme;`
    - Caddy: automatic
- Keep database credentials secure and use strong passwords
- Regularly update to latest version

See [INSTALL.md](INSTALL.md) for detailed deployment options.

## Observability

Trove includes comprehensive monitoring and logging capabilities:

### Health Checks

`GET /health` - Returns server health with database and storage checks

```json
{
  "status": "healthy",
  "version": "dev (commit: abc123, built: 2025-11-24)",
  "checks": {
    "database": {"status": "healthy", "latency": "2.1ms"},
    "storage": {"status": "healthy", "latency": "0.5ms"}
  },
  "uptime": "2h15m30s"
}
```

### Metrics

`GET /metrics` - Prometheus-compatible metrics endpoint

**Security Note:** The metrics endpoint is unauthenticated by default and may expose sensitive operational data (user IDs, request patterns, etc.). In production:
- Use firewall rules to restrict access to trusted IPs
- Place behind a reverse proxy with authentication
- Or bind Trove to `127.0.0.1` and access via SSH tunnel

Available metrics:
- `trove_http_requests_total` - HTTP request counters by method, path, status
- `trove_http_request_duration_seconds` - Request latency histograms
- `trove_http_requests_in_flight` - Current concurrent requests
- `trove_storage_usage_bytes` - Per-user storage consumption
- `trove_files_total` - File upload counters
- `trove_login_attempts_total` - Authentication metrics

Example Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: 'trove'
    static_configs:
      - targets: ['localhost:8080']
```

### Structured Logging

Logs use JSON format in production for machine parsing:

```json
{"time":"2025-11-24T10:30:00Z","level":"INFO","msg":"http request","method":"POST","path":"/upload","status":200,"duration_ms":145}
```

Development uses human-readable text format.

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md)

## Roadmap

**Completed:** Authentication, file management, quotas, CSRF, rate limiting, dark mode, health checks, metrics, structured logging

**Planned:** File sharing links, version history, thumbnails, bulk operations, API

## License

Open source. See LICENSE file.
