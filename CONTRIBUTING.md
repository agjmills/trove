# Contributing to Trove

Thank you for your interest in contributing to Trove! We welcome contributions from the community.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/trove.git
   cd trove
   ```

## Development Setup

```bash
# Copy environment file
cp .env.example .env

# Start development environment
make setup
make dev
```

The app will be available at `http://localhost:8080` with hot-reload enabled.

**Running locally without Docker:**

```bash
# SQLite (simplest)
DB_TYPE=sqlite DB_PATH=./data/trove.db go run ./cmd/server

# In-memory database (ephemeral, useful for quick testing)
DB_TYPE=sqlite DB_PATH=:memory: go run ./cmd/server
```

### Makefile targets

```bash
make dev            # Start with hot-reload
make test           # Run tests
make test-coverage  # Run tests with coverage report
make fmt            # Format code
make build-css      # Rebuild Tailwind CSS
make shell          # Container shell
make psql           # Database console
```

## Architecture

### File Storage Model

Trove separates physical storage from logical organization:

| Field | Purpose | Example |
|-------|---------|---------|
| `StoragePath` | Physical location (UUID-based) | `a48f0152-cbcb-4483.bin` |
| `LogicalPath` | UI folder hierarchy | `/photos/2024` |
| `Filename` | Display name (editable) | `vacation.jpg` |
| `OriginalFilename` | Original upload name (immutable) | `IMG_1234.jpg` |

This design enables:
- **Backend portability**: Move between disk/S3 without changing file references
- **Safe storage**: UUID paths prevent path traversal attacks
- **Flexible organization**: Rename and move files without touching physical storage

### Deduplication

Files are content-addressed by SHA-256 hash. The upload flow ensures duplicates never touch the storage backend:

```
Client → Temp file (computing SHA-256) → Check DB → Storage (if new)
```

1. Upload streams to local temp file while computing hash
2. Database checked for existing file with same hash
3. **If duplicate**: temp file discarded, new DB record points to existing storage path
4. **If new**: temp file uploaded to storage backend
5. Storage quota only charged once per unique file

When deleting files, the physical file is only removed when all references are deleted.

**Note:** Uploads require a writable temp directory. Configure `TEMP_DIR` for containerised deployments.

## Making Changes

### Code Style

- Follow Go conventions (run `make fmt` before committing)
- Write commit messages following [Conventional Commits](https://www.conventionalcommits.org/):
  - `feat: add search pagination`
  - `fix: correct natural sort order for renamed files`
  - `docs: update configuration reference`
  - `refactor: extract sort helpers into shared file`
  - `test: add integration tests for folder sharing`
- Add tests for new functionality
- Update documentation as needed

### Testing

```bash
# Run tests
make test

# Run with coverage
make test-coverage
```

### Building CSS

If you modify Tailwind CSS:

```bash
make build-css
```

## Submitting Changes

1. **Commit your changes**:
   ```bash
   git add .
   git commit -m "feat: description of changes"
   ```

2. **Open a Pull Request** on GitHub with:
   - Clear description of changes
   - Any related issue numbers
   - Screenshots for UI changes

## Pull Request Guidelines

- Keep PRs focused on a single feature/fix
- Include tests for new functionality
- Update README if adding new features
- Ensure all tests pass
- Follow existing code style

## Releases

Releases are automated via GitHub Actions:

- **Docker Images:** Built for `linux/amd64` and `linux/arm64`, pushed to `ghcr.io/agjmills/trove`
  - `main` branch → `latest` tag
  - Version tags (e.g., `v1.0.0`) → corresponding semantic version tags
  - Available at: `ghcr.io/agjmills/trove:latest` or `ghcr.io/agjmills/trove:v1.0.0`

- **Binary Releases:** Created on version tags (`v*`)
  - Linux (amd64, arm64)
  - macOS (amd64, arm64)
  - Windows (amd64)
  - Includes SHA256 checksums
  - Available on the [Releases](https://github.com/agjmills/trove/releases) page

To trigger a release, maintainers create and push a version tag:
```bash
git tag v1.0.0
git push origin v1.0.0
```

## Reporting Issues

Found a bug? Have a feature request?

1. Check if it's already reported in [Issues](https://github.com/agjmills/trove/issues)
2. Open a new issue with:
   - Clear title and description
   - Steps to reproduce (for bugs)
   - Expected vs actual behavior
   - System information (OS, Docker version, etc.)

## Code of Conduct

- Be respectful and inclusive
- Welcome newcomers
- Focus on constructive feedback
- Help make Trove better for everyone

## Questions?

- Open a [Discussion](https://github.com/agjmills/trove/discussions)
- Check existing issues/discussions first

Thank you for contributing! 🎉
