---
title: Install Guide
weight: 2
---

## Docker (recommended)

The quickest way to get running with SQLite:

```bash
mkdir -p trove-data

docker run -d \
  --name trove \
  -p 8080:8080 \
  -v ./trove-data:/app/data \
  -v /tmp \
  -e SESSION_SECRET="$(openssl rand -base64 32)" \
  -e DB_TYPE=sqlite \
  ghcr.io/agjmills/trove:latest
```

> The `-v /tmp` mount is required. The image is scratch-based and has no `/tmp` directory.

Access Trove at `http://localhost:8080`. Register — the first account becomes admin automatically.

## Docker Compose (with PostgreSQL)

For a production setup with a proper database:

**docker-compose.yml**

```yaml
services:
  app:
    image: ghcr.io/agjmills/trove:latest
    restart: unless-stopped
    env_file: .env
    ports:
      - "8080:8080"
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

**.env**

```bash
ENV=production
SESSION_SECRET=        # openssl rand -base64 32
DB_PASSWORD=           # choose a strong password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M

# Required when running behind a reverse proxy
TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

```bash
docker compose up -d
```

## Manual installation

**Prerequisites:** Go 1.21+, Node.js, PostgreSQL 15+ or SQLite

```bash
git clone https://github.com/agjmills/trove.git
cd trove

# Build Tailwind CSS
./build-tailwind.sh

# Build binary
go build -o trove ./cmd/server

# Configure
cp .env.example .env
# Edit .env

./trove
```

PostgreSQL setup:

```sql
CREATE DATABASE trove;
CREATE USER trove WITH PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE trove TO trove;
```

---

## Reverse proxy

Trove is designed to run behind a reverse proxy. In `ENV=production`, session cookies are `Secure`-only, so HTTPS is required.

### Trusted proxy configuration

Set `TRUSTED_PROXY_CIDRS` to your proxy's network subnet. This allows Trove to trust the `X-Forwarded-Proto: https` header from your proxy so CSRF protection and secure cookies work correctly.

```bash
# Find your Docker network subnet
docker network inspect <network_name> | grep Subnet

# .env
TRUSTED_PROXY_CIDRS=172.18.0.0/16

# Multiple networks (comma-separated)
TRUSTED_PROXY_CIDRS=172.17.0.0/16,172.18.0.0/16
```

Without this, logging in under `ENV=production` will silently fail — the session cookie is set as `Secure` but the connection isn't recognised as HTTPS.

### Caddy

Caddy handles HTTPS automatically and sets the required headers with no extra configuration:

```caddy
trove.example.com {
    reverse_proxy localhost:8080
}
```

### Traefik

```yaml
services:
  app:
    image: ghcr.io/agjmills/trove:latest
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.trove.rule=Host(`trove.example.com`)"
      - "traefik.http.routers.trove.entrypoints=websecure"
      - "traefik.http.routers.trove.tls.certresolver=letsencrypt"
    env_file: .env
```

### Nginx

```nginx
server {
    listen 443 ssl;
    server_name trove.example.com;

    ssl_certificate     /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Required for large uploads
        client_max_body_size 0;
        proxy_request_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }
}
```

### Apache

```apache
<VirtualHost *:443>
    ServerName trove.example.com

    SSLEngine on
    SSLCertificateFile    /path/to/cert.pem
    SSLCertificateKeyFile /path/to/key.pem

    ProxyPreserveHost On
    ProxyPass        / http://localhost:8080/
    ProxyPassReverse / http://localhost:8080/

    RequestHeader set X-Forwarded-Proto "https"

    # No body size limit — let Trove enforce MAX_UPLOAD_SIZE
    LimitRequestBody 0
</VirtualHost>
```

---

## S3 / S3-compatible storage

```bash
# AWS S3
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
AWS_REGION=us-east-1
AWS_ACCESS_KEY_ID=...
AWS_SECRET_ACCESS_KEY=...

# MinIO / Cloudflare R2 / Backblaze B2 / rustfs
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
S3_USE_PATH_STYLE=true
AWS_ENDPOINT_URL=http://minio:9000
AWS_ACCESS_KEY_ID=minioadmin
AWS_SECRET_ACCESS_KEY=minioadmin
```

---

## OIDC / SSO

Works with Authentik, Authelia, Keycloak, and any standards-compliant OIDC provider.

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://auth.example.com/application/o/trove/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://trove.example.com/auth/oidc/callback
```

New users are auto-provisioned on first OIDC login. To migrate an existing local account, go to **Admin → Users** and switch that user's identity provider to OIDC.

To grant admin via a claim:

```bash
OIDC_ADMIN_CLAIM=groups
OIDC_ADMIN_VALUE=trove-admins
```

---

## Development vs production

| | `ENV=development` | `ENV=production` |
|---|---|---|
| Session cookies | Not `Secure` (HTTP ok) | `Secure` (HTTPS required) |
| CSRF origin check | Disabled | Enabled |
| Error detail | Full-stack traces | Minimal messages |

For local development without a reverse proxy, use `ENV=development`.
