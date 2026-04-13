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
- 🔗 File sharing links with optional expiry, use limits, and password protection
- 📂 Folder sharing links with the same controls
- 🔍 Full-text file search with tag support
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
| **OIDC/SSO** | ✅ | ✅ (app) | ✅ (Pro) |
| **File & folder sharing** | ✅ expiry, limits, password | ✅ | ✅ |
| **Full-text search** | ✅ with tags | ✅ (plugin) | ✅ |
| **WebDAV** | Planned | ✅ | ✅ |
| **Focus** | File storage only | Full office suite | File sync + sharing |

If you just want to store, organize, and share files — without a calendar, contacts app, or office suite — Trove is for you.

## Quick Start

**Prerequisites:** Docker and Docker Compose

Create a `docker-compose.yml`:

```yaml
services:
  app:
    image: ghcr.io/agjmills/trove:latest
    restart: unless-stopped
    env_file: .env
    volumes:
      - ./data:/app/data
      - /tmp
    depends_on:
      postgres:
        condition: service_healthy

  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      - POSTGRES_DB=trove
      - POSTGRES_USER=trove
      - POSTGRES_PASSWORD=${DB_PASSWORD}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U trove"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  postgres-data:
```

Create a `.env`:

```bash
ENV=production
SESSION_SECRET=        # openssl rand -base64 32
DB_PASSWORD=           # choose a strong password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M

# Required when running behind a reverse proxy (Traefik, Nginx, Caddy, etc.)
# Set to your proxy's Docker network CIDR — check with: docker network inspect <network>
TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

Then:

```bash
docker compose up -d
```

Trove will be available at `http://localhost:8080`. The first account you register becomes admin — go to `/register` to set it up.

Multi-arch images available for `linux/amd64` and `linux/arm64`.

> For reverse proxy setup (Traefik, Nginx, Caddy), OIDC, S3 storage, and troubleshooting, see the **[install guide](https://agjmills.github.io/trove/docs/install/)**.

## First-time Setup

1. Deploy and go to `/register` — the first account becomes admin automatically
2. Set `ENABLE_REGISTRATION=false` in `.env` and restart to lock down signups
3. Add other users via the admin panel, or let them log in via OIDC

If you're using OIDC only with `ENABLE_REGISTRATION=false`, the first OIDC login on a fresh database auto-provisions an admin account — you won't get locked out.

> The admin panel prevents you from switching your *own* account to OIDC while logged in, to stop you accidentally locking yourself out. Another admin can do it, or you can set the `identity_provider` column directly in the database.

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

## OIDC / SSO

Trove supports OIDC for single sign-on with Authentik, Authelia, Keycloak, or any other OIDC-compatible provider.

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://auth.example.com/application/o/trove/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://trove.example.com/auth/oidc/callback
```

New users are auto-provisioned on first OIDC login. To migrate an existing local account to OIDC:

1. In the admin panel go to **Users** and find the account
2. Switch their identity provider from `Internal` to `OIDC`
3. They log in via OIDC — their subject gets linked and local password auth is disabled for that account

| Variable | Default | Description |
|---|---|---|
| `OIDC_ENABLED` | `false` | Enable OIDC |
| `OIDC_ISSUER_URL` | | Provider discovery URL (include trailing slash for Authentik) |
| `OIDC_CLIENT_ID` | | Client ID |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_REDIRECT_URL` | | Callback URL (`https://your-trove/auth/oidc/callback`) |
| `OIDC_SCOPES` | `openid email profile` | Scopes to request |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | Claim to use as username |
| `OIDC_EMAIL_CLAIM` | `email` | Claim to use as email |
| `OIDC_ADMIN_CLAIM` | | Claim that controls admin status |
| `OIDC_ADMIN_VALUE` | | Value that grants admin (e.g. `admins`) |

`OIDC_ADMIN_CLAIM` handles string, array (Authentik/Keycloak groups), and boolean claim shapes automatically.

## Configuration

```bash
# Server
TROVE_PORT=8080
ENV=production                         # development or production

# Database
DB_TYPE=postgres                       # postgres or sqlite
DB_HOST=postgres
DB_NAME=trove
DB_USER=trove
DB_PASSWORD=secret

# Storage
STORAGE_BACKEND=disk                   # disk, s3, or memory
STORAGE_PATH=./data/files              # for disk backend
TEMP_DIR=/tmp                          # temp directory for uploads

# S3 (if STORAGE_BACKEND=s3)
S3_BUCKET=trove
S3_USE_PATH_STYLE=false                # true for MinIO/rustfs
# AWS_REGION=us-east-1
# AWS_ACCESS_KEY_ID=...
# AWS_SECRET_ACCESS_KEY=...
# AWS_ENDPOINT_URL=http://localhost:9000

# Limits
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M

# Security
SESSION_SECRET=change-in-production    # openssl rand -base64 32
CSRF_ENABLED=true

# Reverse proxy — set to your proxy's Docker network CIDR
# Production mode requires this when running behind a proxy
# Find it with: docker network inspect <network_name> | grep Subnet
TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

See `.env.example` for all options and the [install guide](https://agjmills.github.io/trove/docs/install/) for detailed deployment guides including reverse proxy setup for Traefik, Nginx, and Caddy.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for local setup, architecture notes, and how to submit changes.

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

### Search

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/search` | Search files by name, content, or tag |

### File Sharing

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/files/{id}/share` | Create a share link |
| `POST` | `/share/{token}/revoke` | Revoke a share link |
| `GET` | `/s/{token}` | Browse/download via share link (public) |
| `POST` | `/s/{token}` | Submit password for a protected share link |

### Folder Sharing

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/folders/share` | Create a folder share link |
| `POST` | `/f/{token}/revoke` | Revoke a folder share link |
| `GET` | `/f/{token}` | Browse shared folder (public) |
| `POST` | `/f/{token}` | Submit password for a protected folder link |
| `GET` | `/f/{token}/files/{id}` | Download a file from a shared folder |

### Chunked Upload API

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/uploads/init` | Initialise a chunked upload |
| `POST` | `/api/uploads/{id}/chunk` | Upload a chunk |
| `POST` | `/api/uploads/{id}/complete` | Finalise upload |
| `DELETE` | `/api/uploads/{id}` | Cancel upload |
| `GET` | `/api/uploads/{id}/status` | Poll upload status |
| `GET` | `/api/files/status` | SSE stream for upload progress |

### System

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/health` | Health check |
| `GET` | `/metrics` | Prometheus metrics |

## File Sharing

Share links let you hand a direct download URL to anyone — no account required on their end.

Open any file's detail page and expand **Create share link**. You can optionally set:

- **Expiry date** — the link stops working after the end of that day (UTC)
- **Max uses** — the link stops working after N downloads
- **Password** — recipients must enter the correct password before downloading

The share URL looks like `https://your-trove/s/<token>`. Copy it from the Sharing section with one click.

To revoke a link before it expires or runs out of uses, hit **Revoke** next to it. Revoking is instant and permanent.

**Security notes:**
- Tokens are 32-byte cryptographically random values — they cannot be guessed
- Expired, exhausted, and revoked links all return 404 — no information leakage
- Only the file owner can create or revoke share links for their files
- Use count is incremented atomically — concurrent requests cannot bypass a max-uses limit
- Passwords are bcrypt-hashed — the plaintext is never stored

## Folder Sharing

You can share an entire folder as a browseable link — recipients can navigate the folder tree and download individual files without an account.

Go to **Files**, open the folder menu, and choose **Share folder**. The same optional controls apply:

- **Expiry date** — the link stops working after the end of that day (UTC)
- **Max uses** — the link stops working after N visits to the folder root
- **Password** — recipients must enter the correct password before browsing

The share URL looks like `https://your-trove/f/<token>`.

To manage or revoke folder share links, go to **Folders → Manage shares**. Revoking is instant and permanent.

## Security

For security-related documentation including CSRF protection details and migration notes, see [SECURITY.md](SECURITY.md).

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md).

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
- ✅ File sharing links (expiry, use limits, password protection)
- ✅ Folder sharing links (expiry, use limits, password protection)
- ✅ Full-text search with tag support

**Planned:**
- WebDAV endpoint (`/dav/`) — mount as a network drive
- App-specific passwords (for OIDC users and automation)
- Bulk operations (multi-select delete/move/download)
- ZIP download for folders
- Version history

## License

Open source. See LICENSE file.
