# Contributing to ojs-go-sdk

Thank you for your interest in contributing to the OJS Go SDK.

## Getting Started

1. Fork the repository
2. Clone your fork and create a branch from `main`
3. Make your changes
4. Run `make test` and `make vet` before submitting

## Development

**Prerequisites:** Go 1.22+

```bash
# Run tests with race detection
make test

# Run tests with coverage report
make cover

# Run linting
make lint
```

## Code Guidelines

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep the SDK dependency-free (standard library only)
- Add tests for new functionality -- target 80%+ coverage on new code
- Use the existing functional options pattern (`With*`) for configuration
- Exported functions and types must have godoc comments
- Error handling should follow the existing `errors.Is`/`errors.As` patterns

## Commit Messages

We use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(client): add bulk cancel operation
fix(worker): prevent goroutine leak on shutdown
test(workflow): add Group/Batch workflow tests
docs(readme): update quick start examples
```

## Pull Requests

- Keep PRs focused on a single change
- Include tests for bug fixes and new features
- Update documentation if the public API changes
- All CI checks must pass

## Reporting Issues

When reporting bugs, include:
- Go version (`go version`)
- OJS server version and backend
- Minimal reproduction steps

## Discussions

For questions, ideas, and RFCs, use [GitHub Discussions](https://github.com/openjobspec/ojs-go-sdk/discussions).
Reserve issues for actionable bug reports and feature requests.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 license.
