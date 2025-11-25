<p align="center">
  <img src="trove.svg" alt="Trove Logo" width="150" height="150">
  <br />
</p>

<h1 align="center">Trove</h1>

Self-hosted file storage. Your personal Google Drive alternative.

**Simple. Fast. Extensible.**

## Features

- 📤 Upload, organize, and manage files with drag-and-drop
- 📦 Streaming uploads for large files (multi-GB support)
- 💾 Pluggable storage backends (local disk, S3, in-memory)
- 🔄 Content-addressed deduplication (saves storage space)
- 👥 Multi-user support with authentication and per-user quotas
- 📁 Virtual folder hierarchy with file organization
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

## Storage Backends

Trove supports multiple storage backends, configured via the `STORAGE_BACKEND` environment variable.

### Local Disk (Default)

Stores files on the local filesystem with path traversal protection using Go 1.23+ `os.Root`.

```bash
STORAGE_BACKEND=disk
STORAGE_PATH=./data/files
```

### Amazon S3 / S3-Compatible

Stores files in S3 or any S3-compatible service (MinIO, Cloudflare R2, Backblaze B2, rustfs).

```bash
STORAGE_BACKEND=s3
S3_ENDPOINT=https://s3.amazonaws.com    # Or custom endpoint for S3-compatible
S3_REGION=us-east-1
S3_BUCKET=my-trove-bucket
S3_ACCESS_KEY=your-access-key
S3_SECRET_KEY=your-secret-key
S3_USE_PATH_STYLE=false                 # true for MinIO/rustfs
```

**Local development with rustfs:**

```bash
# Start rustfs (S3-compatible storage)
docker compose --profile s3 up rustfs -d

# Create the bucket
AWS_ACCESS_KEY_ID=rustfsadmin AWS_SECRET_ACCESS_KEY=rustfsadmin \
  aws --endpoint-url http://localhost:9000 s3 mb s3://trove

# Run Trove with S3 backend
STORAGE_BACKEND=s3 \
S3_ENDPOINT=http://localhost:9000 \
S3_BUCKET=trove \
S3_ACCESS_KEY=rustfsadmin \
S3_SECRET_KEY=rustfsadmin \
S3_USE_PATH_STYLE=true \
go run ./cmd/server
```

### In-Memory (Testing)

Stores files in memory. Useful for integration tests. Data is lost on restart. Best used in conjunction with sqlite in memory mode for metadata.

```bash
STORAGE_BACKEND=memory
```

## Architecture

### File Storage Model

Trove separates physical storage from logical organization:

| Field | Purpose | Example |
|-------|---------|---------|
| `StoragePath` | Physical location (UUID-based) | `a48f0152-cbcb-4483.bin` |
| `LogicalPath` | UI folder hierarchy | `/photos/2024` |
| `Filename` | Display name (editable) | `vacation.jpg` |
| `OriginalFilename` | Original upload name (immutable) | `IMG_1234.jpg` |

This design enables:
- **Backend portability**: Move between disk/S3 without changing file references
- **Safe storage**: UUID paths prevent path traversal attacks
- **Flexible organization**: Rename and move files without touching physical storage

### Deduplication

Files are content-addressed by SHA-256 hash. The upload flow ensures duplicates never touch the storage backend:

```
Client → Temp file (computing SHA-256) → Check DB → Storage (if new)
```

1. Upload streams to local temp file while computing hash
2. Database checked for existing file with same hash
3. **If duplicate**: temp file discarded, new DB record points to existing storage path
4. **If new**: temp file uploaded to storage backend
5. Storage quota only charged once per unique file

When deleting files, the physical file is only removed when all references are deleted.

**Note:** Uploads require a writable temp directory. Configure `TEMP_DIR` for containerized deployments (see Configuration).

## Development

```bash
make dev      # Start with hot-reload
make test     # Run tests
make shell    # Container shell
make psql     # Database console
```

**Running locally without Docker:**

```bash
# SQLite (simplest)
DB_TYPE=sqlite DB_PATH=./data/trove.db go run ./cmd/server

# In-memory database (ephemeral)
DB_TYPE=sqlite DB_PATH=:memory: go run ./cmd/server
```

## Configuration

Edit `.env` for your setup:

```bash
# Server
TROVE_PORT=8080
ENV=development                        # or production

# Database
DB_TYPE=postgres                       # or sqlite
DB_HOST=postgres
DB_NAME=trove
DB_USER=trove
DB_PASSWORD=secret

# Storage
STORAGE_BACKEND=disk                   # disk, s3, or memory
STORAGE_PATH=./data/files              # for disk backend
TEMP_DIR=/tmp                          # temp directory for uploads

# S3 (if STORAGE_BACKEND=s3)
S3_ENDPOINT=http://localhost:9000
S3_REGION=us-east-1
S3_BUCKET=trove
S3_ACCESS_KEY=rustfsadmin
S3_SECRET_KEY=rustfsadmin
S3_USE_PATH_STYLE=true

# Limits
DEFAULT_USER_QUOTA=10G                 # Per-user storage limit
MAX_UPLOAD_SIZE=500M                   # Max file size per upload

# Security
SESSION_SECRET=change-in-production
CSRF_ENABLED=true
```

See `.env.example` for all options.

## Production

```bash
# 1. Setup
cp .env.example .env
cp docker-compose.example.yml docker-compose.prod.yml

# 2. Configure
# Edit .env with production values (strong SESSION_SECRET, etc.)

# 3. Deploy
docker compose -f docker-compose.prod.yml up -d

# 4. Monitor
docker compose -f docker-compose.prod.yml logs -f
```

**Recommended:** Run behind a reverse proxy (Caddy/Nginx) for HTTPS.

### Docker Volumes

For production Docker deployments, ensure writable volumes for:

```yaml
services:
  app:
    volumes:
      - trove-data:/app/data          # Database and files (disk backend)
      - trove-temp:/tmp               # Temp directory for uploads
    environment:
      - TEMP_DIR=/tmp
```

### Security Best Practices

- Use a strong `SESSION_SECRET` (generate with `openssl rand -base64 32`)
- Restrict `/metrics` endpoint access via firewall or reverse proxy auth
- **Enable HTTPS in production:** Set `ENV=production` for strict CSRF validation
  - Behind reverse proxy with TLS termination: Ensure `X-Forwarded-Proto` header is forwarded
- Keep database credentials secure
- Regularly update to latest version

See [INSTALL.md](INSTALL.md) for detailed deployment options.

## Observability

### Health Checks

`GET /health` - Returns server health with database and storage checks

```json
{
  "status": "healthy",
  "version": "1.0.0 (commit: abc123)",
  "checks": {
    "database": {"status": "healthy", "latency": "2.1ms"},
    "storage": {"status": "healthy", "latency": "0.5ms"}
  },
  "uptime": "2h15m30s"
}
```

### Metrics

`GET /metrics` - Prometheus-compatible metrics endpoint

Available metrics:
- `trove_http_requests_total` - HTTP request counters by method, path, status
- `trove_http_request_duration_seconds` - Request latency histograms
- `trove_http_requests_in_flight` - Current concurrent requests
- `trove_storage_usage_bytes` - Per-user storage consumption
- `trove_files_total` - File upload counters
- `trove_login_attempts_total` - Authentication metrics

**Security Note:** The metrics endpoint is unauthenticated. Restrict access in production.

### Structured Logging

Production uses JSON format:
```json
{"time":"2025-11-24T10:30:00Z","level":"INFO","msg":"http request","method":"POST","path":"/upload","status":200,"duration_ms":145}
```

Development uses human-readable text format.

## API Reference

### Files

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/upload` | Upload file (multipart/form-data) |
| `GET` | `/download/{id}` | Download file |
| `POST` | `/delete/{id}` | Delete file |

### Folders

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/folder/create` | Create folder |
| `POST` | `/folder/delete/{name}` | Delete empty folder |

### System

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md)

## Roadmap

**Completed:**
- Authentication & multi-user support
- File management with streaming uploads
- Storage quotas & deduplication
- Multiple storage backends (disk, S3, memory)
- Virtual folder hierarchy
- CSRF protection & rate limiting
- Health checks & Prometheus metrics
- Structured logging
- Dark mode UI

**Planned:**
- File sharing links
- Version history
- Thumbnail generation
- Bulk operations
- REST API with authentication

## License

Open source. See LICENSE file.
