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
- [x] User/Session/File models with bcrypt-hashed tokens
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
- [ ] Hash-based deduplication

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

### Docker & Deployment
- [x] Dev: Dockerfile + Compose with PostgreSQL, hot reload, health checks
- [ ] Prod: Multi-stage Dockerfile
- [ ] Documentation (README deployment guide)

### Testing & Polish
- [ ] Unit tests for core logic
- [ ] Integration tests for handlers
- [x] Error handling (panic recovery, custom error pages)
- [x] Input validation (CSRF, form validation, file size limits)
- [x] Human-readable size configuration (10G, 500M, etc.)
- [x] Descriptive upload error messages
- [ ] Security headers (X-Frame-Options, CSP, HSTS)
- [ ] Rate limiting on auth endpoints

## Core Features (MVP)

**File Operations**: Upload (single/multiple), download, delete, organize in folders, search by name
**User System**: Registration, cookie-based authentication, per-user storage with quotas, usage tracking
**Storage**: Local filesystem, hash-based deduplication, configurable quotas
**Web UI**: Server-side rendered, responsive, grid/list views, file preview, works without JS
**Security**: Bcrypt passwords, CSRF protection, secure sessions, input validation

## Phase 2 (Optional)
Sharing links, versioning, thumbnails, bulk ops, admin dashboard, REST API

## Tech Stack

**Backend**: Go 1.21+, Chi router, GORM (SQLite/PostgreSQL), bcrypt auth
**Frontend**: Go html/template, Pure CSS or Tailwind, <5KB vanilla JS
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
id, user_id (fk), filename, original_filename, file_path, file_size,
mime_type, hash (SHA-256, indexed), folder_path, created_at, updated_at
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
`POST /files/:id/move`, `/settings`, `POST /settings`, `/storage`

**Admin** (Phase 2): `/admin/users`, `/admin/stats`, `POST /admin/users/:id/quota`

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
STORAGE_PATH=./data/files
DEFAULT_USER_QUOTA=10G            # Supports: B, K/KB, M/MB, G/GB, T/TB
MAX_UPLOAD_SIZE=500M              # Supports: B, K/KB, M/MB, G/GB, T/TB

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
Auth system with dual JSON/form handlers, session management, Docker dev environment, template rendering with layout inheritance, human-readable formatting helpers

### ✅ Phase 2: File Operations (COMPLETE)
Upload/download/delete with quota management, folder organization, drag-and-drop, natural sorting, upload progress bar, pagination (50 items/page)

### ✅ Phase 3: Security & Polish (MOSTLY COMPLETE)
CSRF protection, custom error pages with panic recovery, responsive full-width layout with collapsible sidebar, mobile optimizations

### 🔄 Phase 4: Production Ready (NEXT)
**Priority**: Production Dockerfile → security headers → rate limiting → documentation updates

## Security Status
- [x] Bcrypt password hashing (configurable cost)
- [x] HTTP-only secure session cookies, hashed tokens in DB
- [x] SQL injection prevention (GORM parameterized queries)
- [x] CSRF tokens with validation middleware
- [x] Panic recovery with custom error pages
- [ ] Security headers (X-Frame-Options, CSP, HSTS)
- [ ] Rate limiting on auth endpoints

## Performance Targets
- [ ] Template caching in production
- [ ] Static asset caching headers
- [ ] File streaming (no memory loading)
- [ ] Database indexes on user_id, hash, token_hash
- [ ] <50MB Docker image (production)
- [ ] <100ms page load

---

## Current Status

**Working**: Full authentication system, file upload/download/delete, folder organization, drag-and-drop uploads, upload progress tracking, pagination, CSRF protection, custom error pages, responsive full-width layout with collapsible sidebar, mobile optimizations, human-readable size configuration (10G, 500M), file size validation with descriptive errors

**Next**: Production Dockerfile (multi-stage build), security headers middleware, rate limiting on auth endpoints

**Recent**: Added human-readable size parsing for MAX_UPLOAD_SIZE and DEFAULT_USER_QUOTA (supports B, K/KB, M/MB, G/GB, T/TB), improved upload error messages to show actual server responses and specific error conditions
