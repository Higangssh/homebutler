# Contributing to homebutler

Thanks for your interest in contributing!

## Before submitting a PR

Please run these checks locally before pushing:

```bash
# Format code
gofmt -w .

# Run linter
golangci-lint run

# Run tests
go test ./...

# Build
go build ./...
```

All four must pass. CI will reject PRs that fail any of these.

## Code style

- Follow standard Go conventions
- Run `gofmt` on all `.go` files
- No unused variables or imports
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/) (e.g. `feat:`, `fix:`, `docs:`)

## PR guidelines

- One feature/fix per PR
- Include tests for new functionality
- Update README if adding user-facing features
- Keep PRs small and focused
