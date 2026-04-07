---
title: Install Guide
weight: 2
---

For full reverse proxy setup, OIDC, S3, and troubleshooting see [INSTALL.md](https://github.com/agjmills/trove/blob/main/INSTALL.md) in the repo.

## Reverse proxy

Trove is designed to run behind a reverse proxy. Set `TRUSTED_PROXY_CIDRS` to your proxy's Docker network subnet — find it with:

```bash
docker network inspect <network_name> | grep Subnet
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

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Required for large uploads
        client_max_body_size 0;
        proxy_request_buffering off;
    }
}
```

## S3 / S3-compatible storage

```bash
# AWS S3
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
AWS_REGION=us-east-1

# MinIO / rustfs
STORAGE_BACKEND=s3
S3_BUCKET=my-trove-bucket
S3_USE_PATH_STYLE=true
AWS_ENDPOINT_URL=http://localhost:9000
AWS_ACCESS_KEY_ID=minioadmin
AWS_SECRET_ACCESS_KEY=minioadmin
```

## OIDC / SSO

Works with Authentik, Authelia, Keycloak, and any OIDC-compatible provider.

```bash
OIDC_ENABLED=true
OIDC_ISSUER_URL=https://auth.example.com/application/o/trove/
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://trove.example.com/auth/oidc/callback
```

New users are auto-provisioned on first login. To migrate an existing local account to OIDC, go to the admin panel → Users and switch their identity provider to OIDC.
