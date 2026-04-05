<p align="center">
  <img src="trove.svg" alt="Trove Logo" width="150" height="150">
  <br />
</p>

<h1 align="center">Trove</h1>

<p align="center">
  Self-hosted file storage. Your personal Google Drive alternative.
</p>

<p align="center">
  <a href="https://github.com/agjmills/trove/actions/workflows/ci.yml"><img src="https://github.com/agjmills/trove/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/agjmills/trove" alt="License: MIT"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/agjmills/trove" alt="Go version">
  <a href="https://github.com/agjmills/trove/releases"><img src="https://img.shields.io/github/v/release/agjmills/trove?include_prereleases" alt="Latest release"></a>
  <a href="https://github.com/agjmills/trove/pkgs/container/trove"><img src="https://img.shields.io/badge/docker-ghcr.io-blue" alt="Docker"></a>
</p>

<p align="center"><strong>Simple. Fast. Extensible.</strong></p>

## Features

- 📤 Upload, organize, and manage files with drag-and-drop
- 📦 Streaming uploads for large files (multi-GB support)
- 💾 Pluggable storage backends (local disk, S3, in-memory)
- 🔄 Content-addressed deduplication (saves storage space)
- 👥 Multi-user support with authentication and per-user quotas
- 📁 Virtual folder hierarchy with file organization
- 🗑️ Deleted items with configurable retention (per-user settings)
- 🎨 Tailwind CSS with responsive dark mode (system preference aware)
- 🔒 Secure by default (CSRF protection, bcrypt, rate limiting)
- 🔑 OIDC/SSO support (Authentik, Authelia, Keycloak, etc.)
- 🐳 Easy Docker deployment with multi-arch support
- 🗄️ PostgreSQL or SQLite database options
- 📊 Health checks and Prometheus metrics

## Why Trove?

Trove is for people who want straightforward self-hosted file storage without the overhead of a full cloud suite.

| | Trove | Nextcloud | Seafile |
|---|---|---|---|
| **Setup complexity** | Single Docker container | Multi-container, heavy config | Moderate |
| **Image size** | ~18 MB | ~1 GB+ | ~200 MB |
| **Storage backends** | Disk, S3/R2/B2, MinIO | Disk, S3 (plugin) | Disk, S3 |
| **Tech stack** | Go + SQLite/Postgres | PHP + MySQL | Python + MySQL |
| **Focus** | File storage only | Full office suite | File sync + sharing |

If you just want to store, organize, and share files — without a calendar, contacts app, or office suite — Trove is for you.

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

Uses native AWS SDK environment variables and credential chain:

| Variable | Description |
|----------|-------------|
| `S3_BUCKET` | Bucket name (required) |
| `S3_USE_PATH_STYLE` | Set to `true` for MinIO/rustfs |
| `AWS_REGION` | AWS region |
| `AWS_ACCESS_KEY_ID` | Access key |
| `AWS_SECRET_ACCESS_KEY` | Secret key |
| `AWS_ENDPOINT_URL` | Custom endpoint for S3-compatible services |

The SDK also supports `~/.aws/credentials`, `~/.aws/config`, and IAM roles.

```bash
# AWS S3
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
AWS_REGION=us-east-1

# S3-compatible (MinIO, rustfs)
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
S3_USE_PATH_STYLE=true
AWS_ENDPOINT_URL=http://localhost:9000
AWS_ACCESS_KEY_ID=minioadmin
AWS_SECRET_ACCESS_KEY=minioadmin
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
S3_BUCKET=trove \
S3_USE_PATH_STYLE=true \
AWS_ENDPOINT_URL=http://localhost:9000 \
AWS_ACCESS_KEY_ID=rustfsadmin \
AWS_SECRET_ACCESS_KEY=rustfsadmin \
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

## First-time Setup

The first account registered becomes admin, no seeding required. Just deploy, go to `/register`, and create your account.

If you're running OIDC-only (registration disabled), the first OIDC login on an empty database auto-provisions an admin account instead. Either way, you won't get locked out on a fresh install.

**Typical production flow:**

1. Deploy with `ENABLE_REGISTRATION=true` (the default)
2. Register your admin account at `/register`
3. Set `ENABLE_REGISTRATION=false` to lock signups down
4. Add other users via the admin panel, or let them log in via OIDC and flip their IDP in the Users page

## OIDC / SSO

Trove supports OIDC for single sign-on with Authentik, Authelia, Keycloak, or any other OIDC-compatible provider.

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://auth.example.com/application/o/trove/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://trove.example.com/auth/oidc/callback
```

New users are auto-provisioned on first OIDC login. For existing accounts, an admin switches them from `Internal` to `OIDC` via the Users page — they log in once, the subject gets linked, and local password auth is disabled for that account.

| Variable | Default | Description |
|---|---|---|
| `OIDC_ENABLED` | `false` | Enable OIDC |
| `OIDC_ISSUER_URL` | | Provider discovery URL |
| `OIDC_CLIENT_ID` | | Client ID |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_REDIRECT_URL` | | Callback URL (`/auth/oidc/callback`) |
| `OIDC_SCOPES` | `openid email profile` | Scopes to request |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | Claim to use as username |
| `OIDC_EMAIL_CLAIM` | `email` | Claim to use as email |
| `OIDC_ADMIN_CLAIM` | | Claim that controls admin status |
| `OIDC_ADMIN_VALUE` | | Value that grants admin (e.g. `trove-admins`) |

`OIDC_ADMIN_CLAIM` handles string, array (Authentik/Keycloak groups), and boolean claim shapes automatically.

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

# S3 (if STORAGE_BACKEND=s3) - uses native AWS SDK variables
S3_BUCKET=trove                        # required
S3_USE_PATH_STYLE=false                # true for MinIO/rustfs
# AWS_REGION=us-east-1
# AWS_ACCESS_KEY_ID=...
# AWS_SECRET_ACCESS_KEY=...
# AWS_ENDPOINT_URL=http://localhost:9000  # for S3-compatible services

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

## Security

For security-related documentation including CSRF protection details and migration notes, see [SECURITY.md](SECURITY.md).

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md)

## Roadmap

**Completed:**
- ✅ Authentication & multi-user support
- ✅ File management with streaming uploads
- ✅ Storage quotas & deduplication
- ✅ Multiple storage backends (disk, S3, memory)
- ✅ Virtual folder hierarchy
- ✅ CSRF protection & rate limiting
- ✅ Health checks & Prometheus metrics
- ✅ Structured logging
- ✅ Tailwind CSS with responsive dark mode
- ✅ Production-ready Docker images (~18MB)
- ✅ Deleted items with configurable retention
- ✅ OIDC/SSO authentication

**Planned:**
- File sharing links
- Version history
- Thumbnail generation
- Bulk operations
- REST API with authentication

## License

Open source. See LICENSE file.
