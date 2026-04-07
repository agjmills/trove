---
title: Quick Start
weight: 1
---

**Prerequisites:** Docker and Docker Compose

## 1. Create a docker-compose.yml

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

## 2. Create a .env

```bash
ENV=production
SESSION_SECRET=        # openssl rand -base64 32
DB_PASSWORD=           # choose a strong password
DEFAULT_USER_QUOTA=10G
MAX_UPLOAD_SIZE=500M

# Required when running behind a reverse proxy
TRUSTED_PROXY_CIDRS=172.18.0.0/16
```

## 3. Start it

```bash
docker compose up -d
```

Trove will be available at `http://localhost:8080`.

## 4. Create your account

Go to `/register` — the first account you create becomes admin automatically.

Set `ENABLE_REGISTRATION=false` in `.env` and restart once you're set up.

---

For reverse proxy setup (Traefik, Nginx, Caddy), OIDC, and S3 storage see the [full install guide]({{< ref "install" >}}).
