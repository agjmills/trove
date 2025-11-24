# Trove

Self-hosted file storage. Your personal Google Drive alternative.

**Simple. Fast. Privacy-focused.**

## Features

- 📤 Upload, organize, and manage files with drag-and-drop
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

See [INSTALL.md](INSTALL.md) for detailed deployment options.

## Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md)

## Roadmap

**Completed:** Authentication, file management, quotas, CSRF, rate limiting, dark mode

**Planned:** File sharing links, version history, thumbnails, bulk operations, API

## License

Open source. See LICENSE file.
