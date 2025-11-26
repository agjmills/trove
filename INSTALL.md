# Installation Guide

This guide covers different ways to install and run Trove.

## Table of Contents

- [Docker (Recommended)](#docker-recommended)
- [Docker Compose](#docker-compose)
- [Manual Installation](#manual-installation)
- [Environment Configuration](#environment-configuration)
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
  -v /tmp \
  -e SESSION_SECRET="your-random-secret-here" \
  -e DB_TYPE=sqlite \
  ghcr.io/agjmills/trove:latest
```

> **Note:** The `-v /tmp` mount is required for file uploads as the image uses a minimal scratch base without a `/tmp` directory.

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
  -v /tmp \
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
      - /tmp  # Required for file uploads (scratch-based image)
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
ENV=production
TROVE_PORT=8080
SESSION_SECRET=generate_a_random_32_character_secret
DB_PASSWORD=your_secure_database_password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M
# Required when behind a reverse proxy - set to your proxy's network CIDR
TRUSTED_PROXY_CIDRS=172.18.0.0/16
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

## Environment Configuration

Trove's behavior changes based on the `ENV` setting. Understanding these differences is crucial for proper deployment.

### Development Environment (`ENV=development`)

**Default behavior for local development:**

- Session cookies are **not** marked as Secure (allows HTTP)
- CSRF origin validation is **disabled** (allows localhost testing)
- Detailed error messages and stack traces
- Development-optimized logging

**Example .env:**
```bash
ENV=development
TROVE_HOST=localhost
TROVE_PORT=8080
SESSION_SECRET=change_me_in_production
DB_TYPE=sqlite
```

**Access:** `http://localhost:8080`

### Production Environment (`ENV=production`)

**Secure defaults for production deployment:**

- Session cookies are marked as **Secure** (requires HTTPS)
- CSRF protection with origin validation **enabled**
- Minimal error messages (no stack traces)
- Production-optimized logging

**Important:** Production mode requires either:
1. Direct HTTPS connection to Trove, **OR**
2. Reverse proxy with `X-Forwarded-Proto: https` header **AND** `TRUSTED_PROXY_CIDRS` configured

**Example .env:**
```bash
ENV=production
TROVE_HOST=0.0.0.0
TROVE_PORT=8080
SESSION_SECRET=your_random_32_character_secret_here
DB_TYPE=postgres
DB_HOST=postgres
DB_PASSWORD=your_secure_database_password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M
```

### Key Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ENV` | `development` | Environment mode: `development` or `production` |
| `TROVE_HOST` | `localhost` | Interface to bind to (`0.0.0.0` for all) |
| `TROVE_PORT` | `8080` | Port to listen on |
| `SESSION_SECRET` | (required) | Secret for session encryption (32+ chars) |
| `SESSION_DURATION` | `168h` | Session lifetime (e.g., `24h`, `7d`) |
| `DB_TYPE` | `sqlite` | Database type: `sqlite` or `postgres` |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_NAME` | `trove` | Database name |
| `DB_USER` | `trove` | Database user |
| `DB_PASSWORD` | (required for postgres) | Database password |
| `STORAGE_PATH` | `./data/files` | File storage directory |
| `DEFAULT_USER_QUOTA` | `1GB` | Default storage quota per user |
| `MAX_UPLOAD_SIZE` | `100MB` | Maximum file size (supports KB, MB, GB) |
| `REGISTRATION_ENABLED` | `true` | Allow new user registration |
| `TRUSTED_PROXY_CIDRS` | (empty) | Comma-separated CIDRs to trust for X-Forwarded-Proto (e.g., `172.17.0.0/16,10.0.0.0/8`) |

### Security Notes

⚠️ **Never use default `SESSION_SECRET` in production!**

Generate a secure secret:
```bash
# Linux/macOS
openssl rand -base64 32

# Or use any 32+ character random string
```

⚠️ **Production requires HTTPS**

In production mode, session cookies are Secure-only. You must either:
- Run behind a reverse proxy with HTTPS and proper headers
- Use a tool like Caddy for automatic HTTPS

## Reverse Proxy Setup

Running Trove behind a reverse proxy for HTTPS.

### Trusted Proxy Configuration

⚠️ **Security Note:** In production, Trove only trusts `X-Forwarded-Proto` headers from IPs listed in `TRUSTED_PROXY_CIDRS`. This prevents attackers from spoofing the header to bypass CSRF origin checks.

**You must configure `TRUSTED_PROXY_CIDRS`** with your reverse proxy's IP range:

```bash
# Docker default bridge network
TRUSTED_PROXY_CIDRS=172.17.0.0/16

# Multiple networks (comma-separated)
TRUSTED_PROXY_CIDRS=172.17.0.0/16,172.18.0.0/16,10.0.0.0/8

# Single IP (automatically becomes /32)
TRUSTED_PROXY_CIDRS=127.0.0.1
```

Common CIDR values:
- Docker bridge: `172.17.0.0/16`
- Docker Compose: `172.18.0.0/16` (or check with `docker network inspect`)
- Kubernetes pod network: varies by CNI (e.g., `10.244.0.0/16` for Flannel)
- Localhost: `127.0.0.1/32`

If `TRUSTED_PROXY_CIDRS` is not set, Trove falls back to checking `r.TLS` (direct TLS connections only).

### Requirements for Production Mode

When running with `ENV=production`, your reverse proxy **must** set:

```
X-Forwarded-Proto: https
```

This header tells Trove the original request came via HTTPS, enabling proper CSRF protection and secure cookie handling.

### Caddy (Recommended)

Automatic HTTPS with Let's Encrypt. Caddy automatically sets required headers.

```caddy
files.yourdomain.com {
    reverse_proxy localhost:8080
}
```

That's it! Caddy handles:
- Automatic HTTPS with Let's Encrypt
- `X-Forwarded-Proto` header
- `X-Real-IP` and other standard headers

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
        
        # Required for ENV=production
        proxy_set_header X-Forwarded-Proto $scheme;

        # For large file uploads (set to match MAX_UPLOAD_SIZE or higher)
        client_max_body_size 10G;

        # Disable request buffering for streaming uploads
        proxy_request_buffering off;

        # Extended timeouts for large uploads
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

### Traefik

Docker Compose example with automatic HTTPS:

```yaml
services:
  app:
    image: ghcr.io/agjmills/trove:latest
    environment:
      - ENV=production
      - SESSION_SECRET=${SESSION_SECRET}
      # Trust the Docker network where Traefik runs
      - TRUSTED_PROXY_CIDRS=172.18.0.0/16
    volumes:
      - ./data:/app/data
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.trove.rule=Host(`files.yourdomain.com`)"
      - "traefik.http.routers.trove.entrypoints=websecure"
      - "traefik.http.routers.trove.tls.certresolver=letsencrypt"
      # Traefik automatically sets X-Forwarded-Proto when using websecure entrypoint

  traefik:
    image: traefik:v2.10
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik-config:/etc/traefik
      - ./letsencrypt:/letsencrypt
```

### Apache

```apache
<VirtualHost *:443>
    ServerName files.yourdomain.com
    
    SSLEngine on
    SSLCertificateFile /path/to/cert.pem
    SSLCertificateKeyFile /path/to/key.pem
    
    ProxyPreserveHost On
    ProxyPass / http://localhost:8080/
    ProxyPassReverse / http://localhost:8080/
    
    # Required for ENV=production
    RequestHeader set X-Forwarded-Proto "https"
    
    # For large file uploads
    LimitRequestBody 0
</VirtualHost>
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

### Login fails or session expires immediately (Production)

**Symptoms:**
- Can't login even with correct credentials
- Redirected to login after successful authentication
- Session cookies not being set

**Cause:** Running with `ENV=production` without HTTPS or missing `TRUSTED_PROXY_CIDRS`.

**Solutions:**

1. **If behind reverse proxy:** Ensure both headers AND trusted proxy are configured:
   ```nginx
   # Nginx
   proxy_set_header X-Forwarded-Proto $scheme;
   ```
   ```bash
   # .env - set to your proxy's IP/network
   TRUSTED_PROXY_CIDRS=172.17.0.0/16
   ```

2. **If testing locally:** Change to development mode
   ```bash
   ENV=development
   ```

3. **Find your proxy's network:** 
   ```bash
   # For Docker Compose
   docker network inspect <network_name> | grep Subnet
   ```

### CSRF validation failures

**Symptoms:**
- "CSRF token invalid" errors
- Forms don't submit properly

**Cause:** Misconfigured reverse proxy, missing `TRUSTED_PROXY_CIDRS`, or wrong environment setting.

**Solutions:**

1. **Verify environment:** Check `ENV` in `.env` matches your deployment
   - `development` for local HTTP testing
   - `production` for HTTPS deployments

2. **Check X-Forwarded-Proto:** Must be set to `https` in production

3. **Configure TRUSTED_PROXY_CIDRS:** Required for production behind a proxy
   ```bash
   # .env
   TRUSTED_PROXY_CIDRS=172.17.0.0/16
   ```

4. **SameSite cookies:** Ensure your domain is correct (no subdomain mismatch)

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
