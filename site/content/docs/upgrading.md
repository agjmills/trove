---
title: Upgrading
weight: 10
---

Trove handles database migrations automatically on startup — there's no manual migration step.

## Docker Compose

```bash
docker compose pull
docker compose up -d
```

This pulls the latest image and recreates the container. Downtime is typically a few seconds.

## Docker (standalone)

```bash
docker pull ghcr.io/agjmills/trove:latest
docker stop trove
docker rm trove
docker run -d --name trove ... # same flags as before
```

## Manual (binary)

```bash
git pull
go build -o trove ./cmd/server
./trove
```

## Pinning a version

The `latest` tag always tracks the most recent release. To pin to a specific version:

```yaml
image: ghcr.io/agjmills/trove:v1.2.3
```

Available tags are listed on the [GitHub releases page](https://github.com/agjmills/trove/releases).

## Before upgrading

- **Back up your database.** Migrations are applied automatically and are not reversible without a backup.
- **Back up your storage path** (`STORAGE_PATH`) if using the disk backend.
- Check the [changelog](https://github.com/agjmills/trove/blob/main/CHANGELOG.md) for any breaking changes or new required environment variables.

## Upgrading to v0.11 (video transcoding)

v0.11 adds automatic video transcoding. The web app enqueues jobs for new
video uploads, and a separate **transcoder** container (bundled with ffmpeg)
processes them one at a time into H.264/AAC MP4 (max 720p, faststart) for
in-browser streaming. Originals are retained for downloads.

To enable it:

1. Add the `transcoder` service to your compose file (see the
   [install guide](install.md)) — it must share the app's storage volume and
   database.
2. `docker compose up -d`
3. Convert videos that were already uploaded:

   ```bash
   docker compose run --rm transcoder -backfill
   ```

   Already-compatible MP4s are marked ready without re-encoding; everything
   else is converted in the background. Check progress with
   `docker compose logs -f transcoder`.

The `transcoder` service is optional — without it, uploads and downloads work
exactly as before, only in-browser video playback is unavailable.
