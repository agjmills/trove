---
title: Configuration
weight: 3
---

All configuration is via environment variables, typically in a `.env` file.

## Server

| Variable | Default | Description |
|----------|---------|-------------|
| `TROVE_PORT` | `8080` | Port to listen on |
| `ENV` | `production` | `development` or `production` |
| `ENABLE_REGISTRATION` | `true` | Allow new user registration |
| `TRUSTED_PROXY_CIDRS` | | Proxy network CIDR (required behind a reverse proxy) |

## Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_TYPE` | `postgres` | `postgres` or `sqlite` |
| `DB_HOST` | | PostgreSQL host |
| `DB_NAME` | | Database name |
| `DB_USER` | | Database user |
| `DB_PASSWORD` | | Database password |

## Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `STORAGE_BACKEND` | `disk` | `disk`, `s3`, or `memory` |
| `STORAGE_PATH` | `./data/files` | Path for disk backend |
| `DEFAULT_USER_QUOTA` | `10G` | Default storage quota per user |
| `MAX_UPLOAD_SIZE` | `500M` | Maximum single file upload size |
| `TEMP_DIR` | `/tmp` | Temp directory for uploads |

Sizes support human-readable units: `B`, `K`/`KB`, `M`/`MB`, `G`/`GB`, `T`/`TB`.

## S3 / S3-Compatible

| Variable | Description |
|----------|-------------|
| `S3_BUCKET` | Bucket name |
| `S3_USE_PATH_STYLE` | Set `true` for MinIO or rustfs |
| `AWS_REGION` | AWS region |
| `AWS_ACCESS_KEY_ID` | Access key |
| `AWS_SECRET_ACCESS_KEY` | Secret key |
| `AWS_ENDPOINT_URL` | Custom endpoint for S3-compatible services |

## Security

| Variable | Default | Description |
|----------|---------|-------------|
| `SESSION_SECRET` | | Required. `openssl rand -base64 32` |
| `CSRF_ENABLED` | `true` | Enable CSRF protection |

## OIDC / SSO

| Variable | Default | Description |
|----------|---------|-------------|
| `OIDC_ENABLED` | `false` | Enable OIDC |
| `OIDC_ISSUER_URL` | | Provider discovery URL |
| `OIDC_CLIENT_ID` | | Client ID |
| `OIDC_CLIENT_SECRET` | | Client secret |
| `OIDC_REDIRECT_URL` | | Callback URL (`https://your-trove/auth/oidc/callback`) |
| `OIDC_SCOPES` | `openid email profile` | Scopes to request |
| `OIDC_USERNAME_CLAIM` | `preferred_username` | Claim to use as username |
| `OIDC_EMAIL_CLAIM` | `email` | Claim to use as email |
| `OIDC_ADMIN_CLAIM` | | Claim that controls admin status |
| `OIDC_ADMIN_VALUE` | | Value that grants admin |

## WebDAV

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBDAV_ENABLED` | `false` | Enable WebDAV endpoint at `/dav/` |
