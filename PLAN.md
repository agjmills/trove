# Trove - Homelab File Storage System

## Overview
Self-hostable file storage in Go with server-side rendering, minimal JS, Docker deployment, and SQLite/PostgreSQL support.

## Progress Checklist

### Foundation ✅
- [x] Project structure, Go modules, Chi router, GORM
- [x] PostgreSQL + SQLite fallback with migrations
- [x] Config management (.env), static file serving
- [x] Docker dev environment (Go 1.25 Alpine + Air hot reload)

### Authentication ✅
- [x] User model with bcrypt password hashing
- [x] Session management with alexedwards/scs (SQLite/PostgreSQL stores)
- [x] Registration, Login, Logout (JSON + form support)
- [x] Session middleware (RequireAuth, OptionalAuth, user context)
- [x] CSRF protection

### File Operations ✅
- [x] Upload handler (multipart/form-data, quota checks)
- [x] Download handler (streaming)
- [x] Delete handler (files and folders)
- [x] Folder organization with navigation
- [x] Drag-and-drop upload
- [x] Auto-rename duplicate filenames
- [x] Natural sorting for numbered files
- [x] Upload progress bar with real-time feedback
- [x] List files with pagination (50 per page)
- [x] Hash-based deduplication (SHA-256)
- [x] Streaming uploads for large files (multi-GB support)
- [x] Client-side file size and quota validation
- [x] Rename files and folders
- [x] Move files and folders between directories
- [x] Resumable chunked uploads with pause/resume/cancel
  - Chunked upload API for files > 10MB
  - Automatic retry on network failures
  - Persistent upload sessions with 24h expiration
  - Background cleanup of abandoned uploads
  - SHA-256 hash verification on completion

### Web Interface ✅
- [x] Layout template with navigation
- [x] Login/Register/Dashboard pages with CSS
- [x] Template rendering (separate sets per page)
- [x] Human-readable byte formatting (formatBytes helper)
- [x] Upload form functionality
- [x] Flash messages (error, success, warning, info)
- [x] Breadcrumb navigation
- [x] Parent directory navigation
- [x] Error pages (404, 500) with custom middleware
- [x] Full-width responsive layout with collapsible sidebar
- [x] Mobile optimizations (hidden columns, truncated filenames)

### Docker & Deployment ✅
- [x] Dev: Dockerfile + Compose with PostgreSQL, hot reload, health checks
- [x] Prod: Multi-stage Dockerfile with scratch base (~18MB image)
- [x] Production docker-compose.yml with restart policies
- [x] Documentation (README deployment guide, INSTALL.md)

### Testing & Polish
- [x] Unit tests for core logic (password, config, CSRF, flash, rate limiting, storage)
  - 83 unit tests with 70-90% coverage on tested modules
  - Storage: 82.9% coverage (16 tests)
  - Flash: 89.5% coverage (10 tests)
  - Config: 75.0% coverage (9 tests)
  - CSRF: 70.8% coverage (18 tests)
  - Rate limiter: 39.6% coverage (14 tests)
  - Auth (password): 13.3% coverage (6 tests)
- [x] Integration tests for handlers
  - Auth flows: login, register, logout, password change, settings
  - File operations: upload, download, delete, folders, quota enforcement
  - Page rendering: files page, pagination, folder navigation
  - Routes: health/metrics, public/protected routes, rate limiting, CSRF, session persistence
- [x] Error handling (panic recovery, custom error pages)
- [x] Input validation (CSRF, form validation, file size limits)
- [x] Human-readable size configuration (10G, 500M, etc.)
- [x] Descriptive upload error messages
- [x] Security headers (X-Frame-Options, CSP, HSTS, etc.)
- [x] Rate limiting on auth endpoints (5 attempts per 15 minutes)

## Core Features (MVP)

**File Operations**: Upload (single/multiple), download, delete, organize in folders, search by name
**User System**: Registration, cookie-based authentication, per-user storage with quotas, usage tracking
**Storage**: Local filesystem, hash-based deduplication, configurable quotas
**Web UI**: Server-side rendered, responsive, grid/list views, file preview, works without JS
**Security**: Bcrypt passwords, CSRF protection, secure sessions, input validation

## Phase 2 (Optional)
Sharing links, versioning, thumbnails, bulk ops, REST API

### Admin Dashboard ✅
- [x] Admin role system (first user becomes admin automatically)
- [x] Admin dashboard with system stats (users, files, storage)
- [x] User management (create, delete, toggle admin)
- [x] Storage quota management per user
- [x] Password reset for users
- [x] Background file deletion when deleting users (especially for S3)

## Phase 3 (Enhancements) ✅

### UI/UX Improvements ✅
- [x] **Tailwind CSS Migration**: Replaced custom CSS with Tailwind for easier styling and proper dark mode support
  - Minimal JS philosophy maintained
  - System preference detection + manual toggle
  - Consistent dark/light theme across all pages
  - Responsive design with mobile optimizations

### Storage Abstraction ✅
- [x] **Storage Interface/Adapter Pattern**: Abstract storage layer for multiple backends
  - Interface: `StorageBackend` with methods: `Save()`, `Open()`, `Delete()`, `Stat()`, `HealthCheck()`, `ValidateAccess()`
  - **Disk backend**: Local filesystem with path traversal protection (Go 1.23+ `os.Root`)
  - **S3 backend**: Full AWS S3 and S3-compatible storage support (MinIO, Cloudflare R2, Backblaze B2, rustfs)
  - **Memory backend**: In-memory storage for testing
  - Configuration: `STORAGE_BACKEND=disk|s3|memory` in .env
  - Maintains deduplication support across all backends
  - 8MB copy buffer aligned with S3 multipart upload parts

## Tech Stack

**Backend**: Go 1.21+, Chi router, GORM (SQLite/PostgreSQL), bcrypt auth, alexedwards/scs sessions
**Frontend**: Go html/template, Pure CSS, <5KB vanilla JS
**Deploy**: Docker multi-stage builds, docker-compose, volume mounts

## Project Structure
```
trove/
├── cmd/server/main.go
├── internal/
│   ├── handlers/           # HTTP handlers
│   ├── middleware/         # Auth, CSRF, logging
│   ├── routes/routes.go
│   ├── auth/               # Sessions, password hashing
│   ├── config/config.go
│   ├── database/
│   │   ├── models/         # User, File, Session models
│   │   ├── migrations/
│   │   └── db.go
│   ├── storage/            # File I/O, deduplication
│   └── services/           # Business logic
├── web/
│   ├── static/
│   │   ├── css/style.css
│   │   ├── js/enhance.js   # Optional enhancements
│   │   └── assets/
│   └── templates/
│       ├── layout/base.html
│       └── pages/          # login, dashboard, upload, etc.
├── docker/
│   ├── Dockerfile
│   └── docker-compose.yml
├── .env.example
├── Makefile
└── go.mod
```

## Database Schema

### Users
```sql
id, username (unique), email (unique), password_hash, storage_quota, 
storage_used, is_admin, created_at, updated_at
```

### Files
```sql
id, user_id (fk), filename, original_filename, storage_path, logical_path,
file_size, mime_type, hash (SHA-256, indexed), upload_status, folder_path,
created_at, updated_at, trashed_at
```

### UploadSessions
```sql
id (UUID), user_id (fk), filename, logical_path, total_size, total_chunks,
chunk_size, received_chunks, chunks_received (JSON), status, temp_dir,
created_at, updated_at, expires_at
```

### Sessions
```sql
id, user_id (fk), token_hash, expires_at, created_at, last_used_at
```

### Shares (Phase 2)
```sql
id, file_id (fk), user_id (fk), share_token (unique), expires_at,
download_count, max_downloads, created_at
```

## Web Routes

**Public**: `/` (landing), `/login`, `/register`, `POST /login`, `POST /register`, `POST /logout`

**Protected**: `/dashboard` (file list), `/upload`, `POST /upload`, `/files/:id/view`, 
`/files/:id/download`, `POST /files/:id/delete`, `POST /files/:id/rename`, 
`POST /files/:id/move`, `POST /folders/create`, `POST /folders/rename`,
`POST /folders/move`, `POST /folders/delete/:name`, `/settings`, `POST /settings`, `/storage`

**Upload API**: `POST /api/uploads/init`, `POST /api/uploads/:id/chunk`, 
`POST /api/uploads/:id/complete`, `DELETE /api/uploads/:id`, `GET /api/uploads/:id/status`

**Admin**: `/admin` (dashboard), `/admin/users` (user management), 
`POST /admin/users/create`, `POST /admin/users/:id/toggle-admin`,
`POST /admin/users/:id/quota`, `POST /admin/users/:id/reset-password`,
`POST /admin/users/:id/delete`

## Configuration (.env)

```bash
# Server
PORT=8080
HOST=0.0.0.0
ENV=production

# Database
DB_TYPE=sqlite                    # or postgres
DB_PATH=./data/trove.db           # sqlite only
DB_HOST=localhost                 # postgres only
DB_PORT=5432
DB_NAME=trove
DB_USER=trove
DB_PASSWORD=changeme

# Storage
STORAGE_BACKEND=disk              # or s3, memory
STORAGE_PATH=./data/files
DEFAULT_USER_QUOTA=10G            # Supports: B, K/KB, M/MB, G/GB, T/TB
MAX_UPLOAD_SIZE=500M              # Supports: B, K/KB, M/MB, G/GB, T/TB

# Chunked Uploads
UPLOAD_CHUNK_SIZE=5M              # Default chunk size for resumable uploads
UPLOAD_SESSION_TIMEOUT=24h        # How long upload sessions remain valid

# Security
SESSION_SECRET=changeme_generate_random_secret
SESSION_DURATION=168h             # 7 days
BCRYPT_COST=10
CSRF_ENABLED=true

# Features
ENABLE_REGISTRATION=true
ENABLE_FILE_DEDUPLICATION=true
```

## Docker Setup

**Dockerfile**: Multi-stage (builder + alpine runtime)
**Compose**: Services for app, optional postgres, volume mounts for data/files

## Implementation Status

### ✅ Phase 1: Foundation & Auth (COMPLETE)
Auth system with alexedwards/scs session management (SQLite/PostgreSQL stores), dual JSON/form handlers, Docker dev environment, template rendering with layout inheritance, human-readable formatting helpers

### ✅ Phase 2: File Operations (COMPLETE)
Upload/download/delete with quota management, SHA-256 hash-based deduplication, folder organization, drag-and-drop, natural sorting, upload progress bar, pagination (50 items/page)

### ✅ Phase 3: Security & Testing (COMPLETE)
CSRF protection, custom error pages with panic recovery, responsive full-width layout with collapsible sidebar, mobile optimizations, comprehensive unit tests (83 tests, 70-90% coverage on core modules)

### ✅ Phase 4: Production Ready (COMPLETE)
Production Dockerfile (~18MB), security headers, rate limiting, pluggable storage backends (disk, S3, memory)

### ✅ Phase 5: UI & Polish (COMPLETE)
Tailwind CSS migration with dark mode, responsive design, system preference detection

### 🔄 Phase 6: Documentation & Optimization (NEXT)
**Priority**: Template caching → performance optimization → file sharing links

## Security Status
- [x] Bcrypt password hashing (configurable cost)
- [x] Secure session management via alexedwards/scs (DB-backed, auto-cleanup)
- [x] SQL injection prevention (GORM parameterized queries)
- [x] CSRF tokens with validation middleware
- [x] Panic recovery with custom error pages
- [x] Security headers (X-Frame-Options, CSP, X-Content-Type-Options, Referrer-Policy, Permissions-Policy)
- [x] Rate limiting on auth endpoints (5 attempts/15 min per IP with automatic cleanup)

## Performance Targets
- [ ] Template caching in production
- [ ] Static asset caching headers
- [x] File streaming (no memory loading)
- [x] Database indexes on user_id, hash, token_hash
- [x] <50MB Docker image (production) - **Achieved: ~18MB with scratch base**
- [ ] <100ms page load

---

## Current Status

**Working**: Full authentication system with alexedwards/scs session management, file upload/download/delete with SHA-256 deduplication, folder organization with rename/move operations, drag-and-drop uploads, resumable chunked uploads with pause/resume/cancel, upload progress tracking, pagination, CSRF protection, custom error pages, Tailwind CSS with responsive dark mode, mobile optimizations, human-readable size configuration (10G, 500M), file size validation with descriptive errors, comprehensive security headers, rate limiting on authentication endpoints, production-ready Docker image (~18MB), pluggable storage backends (disk/S3/memory), comprehensive unit tests (90+ tests, 70-90% coverage), streaming uploads for multi-GB files, health checks and Prometheus metrics, admin dashboard with user management, integration tests for handlers and routes

**Next**: Template caching, performance optimization, file sharing links

**Recent**:
- ✅ Implemented resumable chunked uploads with pause/resume/cancel
  - Minimal REST API for upload management (init, chunk, complete, cancel, status)
  - Client-side chunking with automatic retry (ChunkedUploadManager)
  - Files > 10MB use chunked upload, < 10MB use traditional upload
  - Upload sessions expire after 24h (configurable)
  - Hourly background cleanup of abandoned uploads
  - SHA-256 hash verification on completion
  - Pause/Resume/Cancel UI controls
  - See RESUMABLE_UPLOADS.md for details
- ✅ Added file and folder rename/move operations
  - Rename files and folders via modal dialogs
  - Move files and folders between directories with dropdown selection
  - Maintains logical paths and updates folder structure
- ✅ Added comprehensive integration tests for handlers and routes
- ✅ Implemented admin dashboard with user management
  - First registered user automatically becomes admin
  - Dashboard shows total users, files, storage used
  - User management: create, delete, toggle admin, reset password, update quota
  - Background file deletion when removing users (handles large S3 file counts)
- ✅ Implemented pluggable storage backend system (disk, S3, memory)
- ✅ Full S3 support with AWS SDK v2 (MinIO, Cloudflare R2, Backblaze B2, rustfs compatible)
- ✅ Migrated to Tailwind CSS with dark mode support
- ✅ Implemented streaming uploads for large files (multi-GB support) using direct multipart parsing
- ✅ Added comprehensive health checks and Prometheus metrics
- ✅ Refactored session management to use alexedwards/scs
- ✅ 8MB copy buffer for improved throughput (aligned with S3 multipart parts)
