# Installation Guide

This guide covers different ways to install and run Trove.

## Table of Contents

- [Docker (Recommended)](#docker-recommended)
- [Docker Compose](#docker-compose)
- [Manual Installation](#manual-installation)
- [Reverse Proxy Setup](#reverse-proxy-setup)

## Docker (Recommended)

The easiest way to run Trove.

### Quick Start

```bash
# Create data directory
mkdir -p trove-data

# Run Trove
docker run -d \
  --name trove \
  -p 8080:8080 \
  -v trove-data:/app/data \
  -e SESSION_SECRET="your-random-secret-here" \
  -e DB_TYPE=sqlite \
  ghcr.io/agjmills/trove:latest
```

Access Trove at `http://localhost:8080`

### With PostgreSQL

```bash
# Start PostgreSQL
docker run -d \
  --name trove-db \
  -e POSTGRES_DB=trove \
  -e POSTGRES_USER=trove \
  -e POSTGRES_PASSWORD=secure_password \
  -v trove-db:/var/lib/postgresql/data \
  postgres:15-alpine

# Start Trove
docker run -d \
  --name trove \
  --link trove-db \
  -p 8080:8080 \
  -v trove-data:/app/data \
  -e SESSION_SECRET="your-random-secret-here" \
  -e DB_TYPE=postgres \
  -e DB_HOST=trove-db \
  -e DB_PASSWORD=secure_password \
  ghcr.io/agjmills/trove:latest
```

## Docker Compose

For a complete setup with database and persistence.

### Development

```bash
# Clone repository
git clone https://github.com/agjmills/trove.git
cd trove

# Configure
cp .env.example .env
# Edit .env with your settings

# Start
make setup
```

### Production

1. **Create docker-compose.yml**:

```yaml
services:
  app:
    image: ghcr.io/agjmills/trove:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    env_file:
      - .env
    depends_on:
      - postgres
    restart: unless-stopped

  postgres:
    image: postgres:15-alpine
    environment:
      - POSTGRES_DB=trove
      - POSTGRES_USER=trove
      - POSTGRES_PASSWORD=${DB_PASSWORD}
    volumes:
      - postgres-data:/var/lib/postgresql/data
    restart: unless-stopped

volumes:
  postgres-data:
```

2. **Create .env**:

```bash
TROVE_PORT=8080
SESSION_SECRET=generate_a_random_32_character_secret
DB_PASSWORD=your_secure_database_password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M
```

3. **Start**:

```bash
docker compose up -d
```

## Manual Installation

For advanced users who want to run without Docker.

### Prerequisites

- Go 1.21+
- PostgreSQL 15+ or SQLite
- Node.js (for building CSS)

### Steps

1. **Clone and build**:

```bash
git clone https://github.com/agjmills/trove.git
cd trove

# Build Tailwind CSS
./build-tailwind.sh

# Build Go binary
go build -o trove ./cmd/server
```

2. **Setup database** (PostgreSQL):

```sql
CREATE DATABASE trove;
CREATE USER trove WITH PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE trove TO trove;
```

3. **Configure**:

```bash
cp .env.example .env
# Edit .env with your settings
```

4. **Run**:

```bash
./trove
```

## Reverse Proxy Setup

Running Trove behind a reverse proxy for HTTPS.

### Caddy (Recommended)

Automatic HTTPS with Let's Encrypt:

```caddy
files.yourdomain.com {
    reverse_proxy localhost:8080
}
```

### Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name files.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        
        # For large file uploads
        client_max_body_size 500M;
    }
}
```

### Traefik

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.trove.rule=Host(`files.yourdomain.com`)"
  - "traefik.http.routers.trove.entrypoints=websecure"
  - "traefik.http.routers.trove.tls.certresolver=letsencrypt"
```

## Post-Installation

1. **Access Trove** at your configured URL
2. **Create first user** (if registration is enabled)
3. **Test upload/download**
4. **Configure quotas** as needed
5. **Setup backups**

## Troubleshooting

### Port already in use
Change `TROVE_PORT` in `.env` to a different port.

### Permission denied
Ensure data directory has correct permissions:
```bash
chmod 755 data
```

### Database connection failed
Check database credentials in `.env` match your database setup.

### Can't upload files
Check `MAX_UPLOAD_SIZE` setting and reverse proxy limits.

## Updating

### Docker
```bash
docker pull ghcr.io/agjmills/trove:latest
docker compose up -d
```

### Manual
```bash
git pull
./build-tailwind.sh
go build -o trove ./cmd/server
./trove
```

## Next Steps

- Set up regular backups
- Configure user quotas
- Setup monitoring
- Review security settings

For more help, see the [main README](README.md) or open an issue on GitHub.
