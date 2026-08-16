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

## Video Transcoding

Video uploads are converted in the background by the **transcoder worker**
(separate container/binary with ffmpeg) into H.264/AAC MP4, max 1280x720,
`+faststart`, so they can stream in the browser. The original file is always
kept for downloads and the MP4 variant counts toward the user's storage quota.

| Variable | Default | Description |
|----------|---------|-------------|
| `TRANSCODE_ENABLED` | `true` | Enqueue transcode jobs on video upload (web server) |
| `TRANSCODE_POLL_INTERVAL` | `5s` | How often the worker polls for new jobs |
| `TRANSCODE_WORKERS` | `1` | Concurrent jobs (ffmpeg is CPU-heavy) |
| `TRANSCODE_MAX_ATTEMPTS` | `3` | Retries before a job is marked failed |
| `TRANSCODE_TIMEOUT` | `2h` | Per-job timeout |
| `TRANSCODE_PRESET` | `medium` | libx264 preset (`ultrafast`..`veryslow`) |
| `TRANSCODE_CRF` | `23` | Quality value (lower = better, larger files) |
| `TRANSCODE_MAX_HEIGHT` | `720` | Maximum output height in pixels |
| `TRANSCODE_THREADS` | `0` | Cap ffmpeg thread count (`0` = auto; e.g. `4` to limit CPU) |
| `TRANSCODE_STALE_JOB_AGE` | `30m` | Re-queue jobs stuck in `processing` after this long |
| `FFMPEG_PATH` | `ffmpeg` | ffmpeg binary location (worker only) |
| `FFPROBE_PATH` | `ffprobe` | ffprobe binary location (worker only) |

To convert videos uploaded before the transcoder was added, run the backfill
once:

```bash
docker compose run --rm transcoder -backfill
```

**Tips:**

- Set `TRANSCODE_THREADS` to limit CPU on shared hosts (a single job can
  otherwise saturate all cores).
- Use `TRANSCODE_PRESET=veryfast` for weaker hardware; `slow` for smaller
  files at the cost of much longer encodes.
- The worker must share the same `STORAGE_*` configuration and storage volume
  as the web server, and its `TEMP_DIR` needs room for roughly twice the size
  of the largest video being converted.

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

