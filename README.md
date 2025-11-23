# Trove

Self-hosted file storage in Go.

## Features

- User authentication and sessions
- File upload/download with quota management
- PostgreSQL or SQLite
- Hash-based deduplication
- Docker deployment

## Development

Requires Docker and Docker Compose.

```bash
make setup    # first time setup
make dev      # start and watch logs
make shell    # open container shell
make psql     # postgres console
```

Server runs at `http://192.168.0.3:5014`

## Configuration

Copy `.env.example` to `.env`. Main options:

```bash
DB_TYPE=postgres              # or sqlite
STORAGE_PATH=./data/files
DEFAULT_USER_QUOTA=10737418240
SESSION_SECRET=changeme
```

## Status

Early development. Auth and file uploads coming next.

## License

See LICENSE
