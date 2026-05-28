# Contributing to nv-config

Thank you for your interest in contributing to nv-config! This document provides guidelines and information for contributors.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Testing](#testing)
- [Code Style](#code-style)
- [Submitting Changes](#submitting-changes)
- [Release Process](#release-process)

## Code of Conduct

By participating in this project, you agree to abide by our Code of Conduct. Please treat all contributors and users with respect.

## Getting Started

### Prerequisites

- Go 1.24.3 or later
- Git
- [asdf](https://asdf-vm.com/) (recommended for version management)

### Fork and Clone

1. Fork the repository on GitLab
2. Clone your fork:
   ```bash
   git clone https://gitlab-master.nvidia.com/ncp/vmaas/libs/golang/nv-config.git
   cd nv-config
   ```
3. Add the original repository as upstream:
   ```bash
   git remote add upstream gitlab-master.nvidia.com?owner=ncp/vmaas/libs/golang&repo=nv-config.git
   ```

## Development Setup

1. Install Go version using asdf (if you have it):
   ```bash
   asdf install
   ```

2. Install development tools:
   ```bash
   make install-tools
   ```

3. (Optional) Set up pre-commit hooks:
   ```bash
   make setup-pre-commit
   ```

4. Download dependencies:
   ```bash
   make deps
   ```

5. Verify everything works:
   ```bash
   make test
   ```

## Making Changes

### Branch Naming

Create a descriptive branch name:
- `feature/add-new-function` - for new features
- `fix/handle-edge-case` - for bug fixes
- `docs/update-readme` - for documentation changes
- `refactor/improve-performance` - for refactoring

### Development Workflow

1. Create a new branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes following the [Code Style](#code-style) guidelines

3. Write or update tests for your changes

4. Run the test suite:
   ```bash
   make test
   ```

5. Run all quality checks:
   ```bash
   make ci
   # or run pre-commit checks
   make pre-commit-run
   ```

6. Commit your changes with a descriptive message:
   ```bash
   git commit -m "Add new string manipulation function"
   ```

## Testing

We maintain comprehensive test coverage. Please ensure:

### Unit Tests
- Write unit tests for all new functions
- Maintain or improve test coverage
- Run tests: `make test`

### Integration Tests
- Add integration tests for new features
- Run integration tests: `make test-integration`

### Benchmarks
- Add benchmarks for performance-critical code
- Run benchmarks: `make test-bench`

### Test Guidelines
- Use table-driven tests where appropriate
- Test edge cases and error conditions
- Include example functions for documentation
- Ensure tests are deterministic and reliable

## Code Style

### Go Style Guidelines
- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines
- Use `gofmt` for formatting: `make fmt`
- Run `go vet`: `make vet`
- Use `staticcheck` for linting: `make lint`

### Documentation
- Add godoc comments for all exported functions and types
- Include usage examples in godoc
- Update README.md for significant changes
- Update CHANGELOG.md following [Keep a Changelog](https://keepachangelog.com/) format

### Naming Conventions
- Use descriptive names for functions and variables
- Follow Go naming conventions (camelCase for unexported, PascalCase for exported)
- Use consistent terminology throughout the codebase

## Submitting Changes

### Before Submitting
1. Ensure all tests pass: `make test`
2. Run all quality checks: `make ci`
3. Update documentation if needed
4. Update CHANGELOG.md with your changes

### Pull Request Process
1. Push your changes to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

2. Create a Merge Request from your fork to the main repository

3. Fill out the MR template completely:
   - Describe what changes you made
   - Link to any related issues
   - Check all applicable boxes in the template

4. Wait for review and address any feedback

### Review Process
- All changes require review from at least one maintainer
- CI/CD pipeline must pass
- Test coverage should not decrease
- Documentation must be updated for API changes

## Release Process

### Version Numbering
We follow [Semantic Versioning](https://semver.org/):
- `MAJOR.MINOR.PATCH`
- MAJOR: incompatible API changes
- MINOR: backwards-compatible functionality additions
- PATCH: backwards-compatible bug fixes

### Creating Releases
1. Update CHANGELOG.md with release notes
2. Create and push a git tag:
   ```bash
   make tag VERSION=v1.0.0
   ```
3. The CI/CD pipeline will automatically create a GitLab release

## Questions and Support

If you have questions:
1. Check existing issues and documentation
2. Create a new issue with the "question" label
3. Join our discussions in merge requests

## Recognition

Contributors will be recognized in:
- CHANGELOG.md for significant contributions
- README.md contributors section
- Git commit history

Thank you for contributing to nv-config!
