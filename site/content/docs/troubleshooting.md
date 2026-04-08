---
title: Troubleshooting
weight: 9
---

## Can't log in / session expires immediately

**Symptoms:** Login redirects back to the login page, or session cookies aren't set.

**Cause:** Running `ENV=production` without HTTPS, or without `TRUSTED_PROXY_CIDRS` configured.

Production mode sets `Secure` on session cookies — they won't be sent over plain HTTP.

**Fix:**

1. Ensure your reverse proxy sets `X-Forwarded-Proto: https`
2. Set `TRUSTED_PROXY_CIDRS` to your proxy's Docker network subnet:
   ```bash
   TRUSTED_PROXY_CIDRS=172.18.0.0/16
   ```
   Find the subnet with:
   ```bash
   docker network inspect <network_name> | grep Subnet
   ```
3. For local testing, use `ENV=development` instead.

## CSRF token invalid

**Symptoms:** Forms fail with a CSRF error; uploads or settings changes don't go through.

**Cause:** Misconfigured reverse proxy or missing `TRUSTED_PROXY_CIDRS`.

**Fix:** Same as above — ensure `X-Forwarded-Proto: https` is set by your proxy and `TRUSTED_PROXY_CIDRS` covers its IP range.

## Files won't upload

**Cause:** Usually one of:

- `MAX_UPLOAD_SIZE` is smaller than the file
- Your reverse proxy has a body size limit
- The `/tmp` volume isn't mounted (scratch-based Docker image has no `/tmp` by default)

**Fix:**

```bash
# Increase the limit in .env
MAX_UPLOAD_SIZE=10G
```

For Nginx, also set:
```nginx
client_max_body_size 0;
proxy_request_buffering off;
```

For Docker, ensure `/tmp` is mounted:
```yaml
volumes:
  - ./data:/app/data
  - /tmp
```

## Permission denied on data directory

```bash
chmod 755 ./data
```

## Database connection failed

Check that `DB_HOST`, `DB_NAME`, `DB_USER`, and `DB_PASSWORD` in `.env` match your database configuration. For Docker Compose, `DB_HOST` should be the service name (e.g. `postgres`), not `localhost`.

## Port already in use

Change `TROVE_PORT` in `.env` to a free port and restart.

## Share links return 404

The share has likely been revoked, expired, or hit its download limit. Check **Settings → Shares** to confirm.
