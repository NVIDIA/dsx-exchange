# Changelog

All notable changes to the nv-config project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial library structure
- Core `SomeFunction` functionality
- Helper utilities (Capitalize, Reverse, IsEmpty, Sanitize)
- Client with configurable options
- Comprehensive unit tests
- Integration test suite
- Example usage program
- GitLab CI/CD pipeline
- Documentation and README

### Changed

- **Environment Variable Naming**: Environment variables now use logical single-underscore naming with intelligent conversion.
  - **Rationale**: Simplified naming that follows natural conventions. For example, `database.host` maps to `DATABASE_HOST`, `httpclient.timeout` maps to `HTTPCLIENT_TIMEOUT`. This provides intuitive, predictable environment variable names that work seamlessly with nested configuration structures.

### Deprecated

### Removed

### Fixed

- **Configuration Key Parsing**: Ensured underscores (`_`) in configuration keys are handled consistently across all sources (YAML, Environment Variables). This fixes an issue where a YAML key like `database_host` could not be correctly overridden by its corresponding environment variable (`MYSERVICE_DATABASE_HOST`). Keys with underscores are now always treated as flat keys, not nested structures.

### Security

## [0.1.0] - YYYY-MM-DD

### Added

- Initial release of nv-config library
- Basic string processing functionality
- Helper utility functions
- Configurable client with debug and prefix options
- Complete test coverage
- Documentation and examples
- GitLab CI/CD integration
- asdf version management support

[Unreleased]: gitlab-master.nvidia.com?owner=ncp/vmaas/libs/golang&repo=nv-config/compare/v0.1.0...HEAD
[0.1.0]: gitlab-master.nvidia.com?owner=ncp/vmaas/libs/golang&repo=nv-config/releases/tag/v0.1.0
