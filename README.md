# Trove

Self-hosted file storage system built with Go. Simple, fast, and minimal JavaScript.

## Features

- **User Management**: Registration, authentication, and session management
- **File Operations**: Upload (with drag-and-drop), download, delete, folder organization
- **Storage**: Quota management per user, real-time usage tracking, upload progress
- **Security**: CSRF protection, bcrypt password hashing, secure sessions, panic recovery
- **UI**: Responsive full-width layout, collapsible sidebar, mobile-optimized, custom error pages
- **Database**: PostgreSQL or SQLite support with GORM
- **Deployment**: Docker with hot-reload development environment

## Quick Start

Requires Docker and Docker Compose.

```bash
# First time setup
make setup

# Start development server with hot reload
make dev

# Other commands
make shell    # Open container shell
make psql     # PostgreSQL console
make down     # Stop containers
make clean    # Remove containers and volumes
```

Server runs at `http://192.168.0.3:5014`

## Features in Detail

### File Management
- Upload files with drag-and-drop or file picker
- Real-time upload progress tracking
- Organize files in folders with breadcrumb navigation
- Natural sorting for numbered filenames
- Pagination (50 items per page)
- Download and delete operations

### Security
- CSRF protection on all forms
- Bcrypt password hashing (configurable cost)
- HTTP-only secure session cookies
- Session token hashing in database
- Custom error pages with panic recovery
- SQL injection prevention (GORM parameterized queries)

### User Experience
- Full-width responsive layout
- Collapsible sidebar with storage usage bar
- Mobile optimizations (900px breakpoint)
- Flash messages for user feedback
- Works without JavaScript (progressive enhancement)

## Configuration

Copy `.env.example` to `.env` and customize. Key options:

```bash
# Application
PORT=8080
HOST=0.0.0.0

# Database
DB_TYPE=postgres              # or sqlite
DB_HOST=postgres
DB_NAME=trove

# Storage
STORAGE_PATH=./data/files
DEFAULT_USER_QUOTA=10737418240    # 10GB in bytes
MAX_UPLOAD_SIZE=524288000         # 500MB in bytes

# Security
SESSION_SECRET=changeme_generate_random_secret
BCRYPT_COST=10
CSRF_ENABLED=true

# Features
ENABLE_REGISTRATION=true
```

## Current Status

**Completed Features:**
- ✅ Full authentication system with session management
- ✅ File upload/download/delete with quota enforcement
- ✅ Folder organization and navigation
- ✅ Drag-and-drop uploads with progress tracking
- ✅ CSRF protection and custom error pages
- ✅ Responsive UI with mobile optimizations
- ✅ Pagination for file listings

**In Progress:**
- 🔄 Production Dockerfile (multi-stage build)
- 🔄 Security headers middleware
- 🔄 Rate limiting on authentication endpoints

**Future Enhancements:**
- File sharing links
- Version history
- Thumbnail generation
- Bulk operations
- Admin dashboard
- REST API

## License

See LICENSE
