<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# nv-config

Configuration Library

## Overview

A powerful, flexible configuration management library for Go applications that supports multiple configuration sources with automatic precedence handling, type-safe access, and seamless integration with external libraries. Features advanced environment variable support including type-aware conversion and templated variables for dynamic configurations.

### Key Features

- **Simple API**: Easy-to-use constructors for common use cases (`New()`, `NewService()`)
- **Advanced Options**: Fine-grained control with `NewWithOptions()` for power users
- **Multiple Configuration Sources**: YAML files, environment variables, CLI flags
- **Automatic Precedence**: CLI > ENV > Files > Defaults
- **Type-Aware Environment Variables**: Automatic conversion of environment variables based on target field types, including comma-separated strings to `[]string` slices
- **Templated Environment Variables**: Support for dynamic map keys via templated environment variable patterns
- **Environment Variable Mapping**: Environment variables use logical prefixing with single underscores. For example, `database.host` maps to `MYSERVICE_DATABASE_HOST`, `httpclient.timeout` maps to `MYSERVICE_HTTPCLIENT_TIMEOUT`
- **Smart Merge Policy**: Allows precedence-based overrides while preventing library conflicts
- **Plugin System**: External libraries can provide their own configuration defaults
- **Auto-Generated CLI Flags**: Automatically generate Cobra CLI flags from configuration structure with environment variable help text
- **Type-Safe Access**: Strong typing with validation and error handling
- **Hot Reloading**: Watch configuration files for changes
- **Minimal Dependencies**: Clean, focused external dependencies

## Examples

📚 **Learn by Example** - The best way to understand the library is through working examples:

- **[Basic Example](examples/basic/)** - Simple HTTP server with configuration
- **[Service Example](examples/service/)** - Advanced service with library integration and CLI flags
- **[Library Examples](examples/library/)** - How to create configurable libraries

## Documentation

- **[Environment Variables Guide](ENVIRONMENT_VARIABLES.md)** - Complete guide to environment variable usage, including slice fields and templated variables
- **[Contributing Guide](docs/CONTRIBUTING.md)** - Development and contribution guidelines
- **[Changelog](docs/CHANGELOG.md)** - Version history and changes

## Installation

```bash
go get gitlab-master.nvidia.com/ncp/vmaas/services/pkg/nv-config
```

## Quick Start

### Simple Service Configuration

```go
package main

import (
    _ "embed"
    "log"

    "gitlab-master.nvidia.com/ncp/vmaas/services/pkg/nv-config"
)

//go:embed config/defaults.yaml
var defaultConfigYAML string

// ServiceConfig represents your service configuration
type ServiceConfig struct {
    Name    string `koanf:"name"`
    Version string `koanf:"version"`
    Port    int    `koanf:"port"`
}

func main() {
    // New() automatically handles:
    // - Embedded defaults
    // - config.yaml loading
    // - Environment variables (MYSERVICE_ prefix)
    // - Library provider integration
    cfg, err := config.New(defaultConfigYAML, "MYSERVICE", nil)
    if err != nil {
        log.Fatal(err)
    }

    // Load configuration
    if err := cfg.Load(); err != nil {
        log.Fatal(err)
    }

    // Access configuration
    var serviceConfig ServiceConfig
    if err := cfg.Unmarshal(&serviceConfig); err != nil {
        log.Fatal(err)
    }

    log.Printf("Starting %s v%s on port %d",
        serviceConfig.Name, serviceConfig.Version, serviceConfig.Port)
}
```

### With CLI Integration

```go
package main

import (
    _ "embed"
    "log"

    "github.com/spf13/cobra"
    "gitlab-master.nvidia.com/ncp/vmaas/services/pkg/nv-config"
)

//go:embed config/defaults.yaml
var defaultConfigYAML string

var rootCmd = &cobra.Command{
    Use: "myservice",
    Run: runServer,
}

var cfg *config.ConfigManager

func main() {
    // NewService adds CLI flag generation
    var err error
    cfg, err = config.NewService(defaultConfigYAML, "MYSERVICE", nil, rootCmd)
    if err != nil {
        log.Fatal(err)
    }

    rootCmd.Execute()
}

func runServer(cmd *cobra.Command, args []string) {
    // Configuration is automatically loaded
    if err := cfg.Load(); err != nil {
        log.Fatal(err)
    }

    var serviceConfig ServiceConfig
    if err := cfg.Unmarshal(&serviceConfig); err != nil {
        log.Fatal(err)
    }

    log.Printf("Starting %s v%s on port %d",
        serviceConfig.Name, serviceConfig.Version, serviceConfig.Port)
}
```

**config/defaults.yaml:**
```yaml
service:
  name: "My Service"
  version: "1.0.0"
  port: 8080
```

### With Library Integration

```go
// Add library providers for automatic configuration
providers := []config.LibraryConfigProvider{
    &httpclient.ConfigProvider{},
    &database.ConfigProvider{},
}

// Simple library integration (no CLI flags)
cfg, err := config.New(defaultConfigYAML, "MYSERVICE", providers)

// With CLI flags (use NewService)
cfg, err := config.NewService(defaultConfigYAML, "MYSERVICE", providers, rootCmd)

// Now you have:
// - Auto-generated CLI flags: --service-port, --httpclient-timeout, --database-host
// - Environment variables: MYSERVICE_SERVICE_PORT, MYSERVICE_HTTPCLIENT_TIMEOUT
// - Help text shows corresponding env vars: "Configuration for service.port (env: MYSERVICE_SERVICE_PORT)"
// - Type-safe access via GetProviderConfig()
```

### Provider Aliases

Register the same provider multiple times with different aliases for separate configurations:

```go
// Standard providers
providers := []config.LibraryConfigProvider{
    &httpclient.ConfigProvider{},
    &database.ConfigProvider{},
}

// Add aliased providers for additional instances
aliasedProviders := config.ProvidersWithAliases(
    config.ProviderWithAlias{Provider: &httpclient.ConfigProvider{}, Alias: "api-client"},
    config.ProviderWithAlias{Provider: &httpclient.ConfigProvider{}, Alias: "scraper"},
)
providers = append(providers, aliasedProviders...)

cfg, err := config.NewService(defaultConfigYAML, "MYSERVICE", providers, rootCmd)

// Results in separate configuration namespaces:
// httpclient.timeout     -> MYSERVICE_HTTPCLIENT_TIMEOUT     -> --httpclient-timeout
// api-client.timeout     -> MYSERVICE_API_CLIENT_TIMEOUT     -> --api-client-timeout
// scraper.timeout        -> MYSERVICE_SCRAPER_TIMEOUT        -> --scraper-timeout
```


### Advanced Configuration

For advanced use cases requiring fine-grained control, use `NewWithOptions()`:

```go
cfg, err := config.NewWithOptions(
    config.WithDefaultYAML(defaultConfigYAML),
    config.WithEnvPrefix("MYSERVICE"),
    config.WithProviders(providers...),
    config.WithCobraAutoFlags(rootCmd),
    config.WithConfigStruct(ServiceConfig{}), // Enable validation
    config.WithReloadCallback(func() { log.Println("Config reloaded") }),
    config.WithErrorHandler(func(err error) { log.Printf("Config error: %v", err) }),
)
```

## Constructor Guide

The library provides several constructors for different use cases:

| Constructor | Use Case | Features |
|-------------|----------|----------|
| `New(defaultYAML, envPrefix, providers)` | **Recommended** - Simple services | Embedded defaults, config files, env vars, library integration |
| `NewService(defaultYAML, envPrefix, providers, cmd)` | Services with CLI | Everything in `New()` + auto-generated CLI flags |
| `NewForTesting(defaultYAML, providers)` | Unit testing | Embedded defaults + libraries only (no external files) |
| `NewWithOptions(options...)` | Advanced/custom setups | Full control with functional options |

**Choose the right constructor:**
- 🚀 **Start with `New()`** for most applications
- 🖥️ **Use `NewService()`** when you need CLI flags
- 🧪 **Use `NewForTesting()`** in unit tests
- ⚙️ **Use `NewWithOptions()`** for complex custom configurations

## Development

### Prerequisites

- Go 1.24.3+
- [asdf](https://asdf-vm.com/) for version management (optional but recommended)
- [pre-commit](https://pre-commit.com/) for code quality checks (optional but recommended)
- Git for version control

### Setup

1. Clone the repository:

   ```bash
   git clone https://gitlab-master.nvidia.com/ncp/vmaas/services/pkg/nv-config.git
   cd nv-config
   ```

2. If using asdf, install the Go version:

   ```bash
   asdf install
   ```

3. Download dependencies:

   ```bash
   go mod download
   ```

4. Install development tools:

   ```bash
   make install-tools
   ```

5. Check tool installation:

   ```bash
   make check-tools
   ```

   If tools show "⚠️ installed but not in PATH", add Go's bin directory to your PATH:

   ```bash
   export PATH=$PATH:$(go env GOPATH)/bin
   ```

   Add this line to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.) to make it permanent.

6. (Optional) Set up pre-commit hooks:

   ```bash
   make setup-pre-commit
   ```

### Development Workflow

#### Quick Commands

```bash
# Build the library
make build

# Run all tests
make test

# Run integration tests
make test-integration

# Check code quality (format, lint, vet)
make ci

# Run security checks
make security
```

#### Building

```bash
# Build the library
make build

# Build and run example
make run-example
```

#### Testing

```bash
# Run all tests with race detection
make test

# Run tests with verbose output
make test-verbose

# Run integration tests
make test-integration

# Run benchmarks
make test-bench

# Generate coverage report
make coverage
```

#### Code Quality

```bash
# Format code
make fmt

# Lint code (staticcheck)
make lint

# Run go vet
make vet

# Run complete CI pipeline locally
make ci

# Run pre-commit checks
make pre-commit-run
```

#### Development Tools

```bash
# Check tool installation status
make check-tools

# Install all development tools
make install-tools

# Serve documentation locally (http://localhost:6060)
make doc

# Set up git hooks
make init-hooks
```

### Troubleshooting

**Tools not found in PATH:**
If you see "command not found" errors for `staticcheck`, `govulncheck`, etc.:

```bash
# Check if tools are installed
make check-tools

# Add Go bin directory to PATH
export PATH=$PATH:$(go env GOPATH)/bin

# Or reinstall tools
make install-tools
```

**asdf version issues:**
Make sure the correct Go version is active:

```bash
asdf current golang
asdf install golang 1.24.3
asdf local golang 1.24.3
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes and add tests
4. Run the CI pipeline locally: `make ci`
5. Commit your changes (`git commit -m 'Add some amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Merge Request

### Development Guidelines

- **Before committing**: Run `make ci` to ensure code quality
- **Add tests**: All new functionality should have corresponding tests
- **Update docs**: Update documentation for API changes
- **Follow conventions**: Use existing code style and patterns

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
