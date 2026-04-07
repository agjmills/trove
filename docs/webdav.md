# WebDAV Implementation Plan

## Overview

Trove will expose a WebDAV endpoint at `/dav/`, allowing any WebDAV-compatible client (Finder, Windows Explorer, Nautilus, Cyberduck, rclone, Cadaver, etc.) to mount a user's file tree as a network drive — no browser required.

WebDAV is a superset of HTTP, so it works through any existing reverse proxy setup without additional ports or configuration.

---

## Architecture

### Library

Use `golang.org/x/net/webdav` — the standard Go WebDAV package. It provides a `webdav.Handler` that implements all WebDAV HTTP methods (`PROPFIND`, `PROPPATCH`, `MKCOL`, `GET`, `PUT`, `DELETE`, `COPY`, `MOVE`, `LOCK`, `UNLOCK`). We implement the `webdav.FileSystem` interface to back it with Trove's DB + storage layer.

### Authentication

WebDAV clients use **HTTP Basic Auth** — they cannot follow redirect-based login flows. The WebDAV handler authenticates by:

1. Extracting the `Authorization: Basic ...` header
2. Looking up the user by username
3. Verifying the password with bcrypt

**OIDC users cannot use WebDAV** with their primary credentials (they have no local password). The long-term fix is app-specific passwords (a separate `AppPasswords` table with scoped tokens). v1 will skip OIDC users with a `501 Not Implemented` or a clear error.

### File System Mapping

```
WebDAV path          →  Trove concept
/                    →  user root (logical_path = "/")
/photos/             →  folder with logical_path = "/photos"
/photos/dog.jpg      →  file with logical_path = "/photos", filename = "dog.jpg"
```

The `webdav.FileSystem` interface requires five methods:

| Method | Maps to |
|--------|---------|
| `Mkdir(ctx, name, perm)` | `INSERT INTO folders` |
| `OpenFile(ctx, name, flag, perm)` | Read: open from storage backend; Write: buffer to temp then save |
| `RemoveAll(ctx, name)` | Soft-delete file or folder (sets `trashed_at`) |
| `Rename(ctx, oldName, newName)` | Update `logical_path`/`filename` in DB, no storage move needed |
| `Stat(ctx, name)` | Query DB for file or folder metadata |

### Locking

Use `webdav.NewMemLS()` for in-memory locks. This is sufficient for single-instance deployments. Locks are lost on restart but that is acceptable — most clients recover gracefully (they re-lock on reconnect).

Multi-instance deployments (rare for homelabs) would need DB-backed locks; this is a future concern.

---

## New Files

### `internal/handlers/webdav_handler.go`

Core implementation:

```go
type WebDAVHandler struct {
    db      *gorm.DB
    storage storage.StorageBackend
}

// ServeHTTP extracts Basic Auth, verifies credentials, then delegates to webdav.Handler
func (h *WebDAVHandler) ServeHTTP(w http.ResponseWriter, r *http.Request)

// WebDAV FileSystem implementation
func (h *troveFS) Mkdir(ctx context.Context, name string, perm fs.FileMode) error
func (h *troveFS) OpenFile(ctx context.Context, name string, flag int, perm fs.FileMode) (webdav.File, error)
func (h *troveFS) RemoveAll(ctx context.Context, name string) error
func (h *troveFS) Rename(ctx context.Context, oldName, newName string) error
func (h *troveFS) Stat(ctx context.Context, name string) (fs.FileInfo, error)
```

**Write path (PUT):** Buffer incoming bytes to a temp file, compute SHA-256, check deduplication, call `storage.Save`, create `files` record — mirrors the existing upload flow.

**Read path (GET/PROPFIND):** For `Stat` and directory listings, query the DB only. For file reads, open from the storage backend. `webdav.File` requires `io.ReadSeeker`; wrap the storage reader with a temp-file buffer for backends that don't support seeking (S3).

**`troveFileInfo`:** Adapts `models.File` and `models.Folder` to `fs.FileInfo` (Name, Size, ModTime, IsDir, Mode).

### `internal/handlers/webdav_handler_test.go`

Tests using the same per-test SQLite DSN pattern established elsewhere:

- `TestWebDAV_PROPFIND_root` — lists root directory
- `TestWebDAV_MKCOL` — creates a folder
- `TestWebDAV_PUT_and_GET` — uploads then downloads a file
- `TestWebDAV_DELETE` — soft-deletes a file
- `TestWebDAV_MOVE_file` — renames/moves a file
- `TestWebDAV_MOVE_folder` — renames a folder
- `TestWebDAV_BasicAuth_invalid` — wrong password returns 401
- `TestWebDAV_BasicAuth_OIDC_user` — OIDC user returns 403

---

## Modified Files

### `internal/config/config.go`

Add:
```go
WebDAVEnabled bool   // WEBDAV_ENABLED, default false
```

### `internal/routes/routes.go`

```go
if cfg.WebDAVEnabled {
    webdavHandler := handlers.NewWebDAVHandler(db, storageService)
    r.Handle("/dav/*", webdavHandler)
    r.Handle("/dav/", webdavHandler)
}
```

### `go.mod`

Add `golang.org/x/net` (already an indirect dep — promote to direct).

---

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBDAV_ENABLED` | `false` | Enable the WebDAV endpoint |

No additional ports. WebDAV runs on the same port as the web UI under `/dav/`.

---

## Client Setup

### macOS Finder

**Go → Connect to Server** (`⌘K`):
```
http://your-trove/dav/
```
Enter your Trove username and password when prompted.

### Windows Explorer

Map a network drive → `\\your-trove\dav\` (WebDAV over HTTP via the WebClient service).

Or via command line:
```
net use Z: http://your-trove/dav/ /user:username password
```

### Linux (Nautilus / GNOME Files)

**Other Locations → Connect to Server**:
```
davs://your-trove/dav/
```

Or via CLI with `davfs2`:
```bash
mount -t davfs http://your-trove/dav/ /mnt/trove
```

### rclone

```ini
[trove]
type = webdav
url = http://your-trove/dav/
vendor = other
user = username
pass = password  # use rclone obscure
```

```bash
rclone ls trove:
rclone copy ./local-dir trove:backup/
```

### Cyberduck / Mountain Duck

New connection → WebDAV (HTTP) → `your-trove` → Path: `/dav/`

---

## Limitations (v1)

- **OIDC users** cannot authenticate via WebDAV (no local password). Future fix: app-specific passwords.
- **Locks are in-memory** — lost on restart, not suitable for multi-instance deployments.
- **No partial writes** — PUT buffers the full file before saving. Resume-on-reconnect is not supported for large uploads mid-transfer.
- **Quota enforcement** — enforced at PUT time but not pre-checked (no `507 Insufficient Storage` on PROPFIND).
- **Tags** — files uploaded via WebDAV receive no tags. Tags can be added later via the web UI.

---

## Future Work

- **App-specific passwords** — scoped tokens for OIDC users and CI/automation use cases
- **DB-backed locks** — for multi-instance deployments
- **`507 Insufficient Storage`** — return correct error when quota would be exceeded
- **HTTPS enforcement** — Basic Auth over plain HTTP leaks credentials; document strongly that HTTPS is required in production
