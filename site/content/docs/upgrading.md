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
