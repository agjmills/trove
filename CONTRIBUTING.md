# Contributing to Trove

Thank you for your interest in contributing to Trove! We welcome contributions from the community.

## Getting Started

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/YOUR-USERNAME/trove.git
   cd trove
   ```
3. **Create a branch** for your changes:
   ```bash
   git checkout -b feature/my-new-feature
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

## Making Changes

### Code Style

- Follow Go conventions (run `make fmt` before committing)
- Write clear commit messages
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
   git commit -m "Add feature: description of changes"
   ```

2. **Push to your fork**:
   ```bash
   git push origin feature/my-new-feature
   ```

3. **Open a Pull Request** on GitHub with:
   - Clear description of changes
   - Any related issue numbers
   - Screenshots for UI changes

## Pull Request Guidelines

- Keep PRs focused on a single feature/fix
- Include tests for new functionality
- Update README if adding new features
- Ensure all tests pass
- Follow existing code style

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
