# Environment Variables Guide

This document explains how environment variables work in the nv-config library, including syntax, type conversion, and the new slice field support.

## Table of Contents

- [Basic Environment Variable Usage](#basic-environment-variable-usage)
- [Environment Variable Naming](#environment-variable-naming)
- [Type-Aware Conversion](#type-aware-conversion)
- [Slice Fields (Lists)](#slice-fields-lists)
- [Templated Environment Variables](#templated-environment-variables)
- [Validation](#validation)
- [Examples](#examples)
- [Best Practices](#best-practices)

## Basic Environment Variable Usage

Environment variables are automatically loaded when you configure the library with an environment prefix:

```go
cm, err := config.NewWithOptions(
    config.WithEnvPrefix("MYAPP"),  // Short, unique prefix for your app
    config.WithConfigStruct(MyConfig{}),
)
```

### Configuration Structure to Environment Variable Mapping

The library automatically maps struct fields to environment variables based on their `koanf` tags:

```go
type ServerConfig struct {
    Host     string `koanf:"host"`
    Port     int    `koanf:"port"`
    Features []string `koanf:"features"`
}

type Config struct {
    Server ServerConfig `koanf:"server"`
}
```

This creates the following environment variable mappings:
- `MYAPP_SERVER_HOST` → `config.Server.Host`
- `MYAPP_SERVER_PORT` → `config.Server.Port`
- `MYAPP_SERVER_FEATURES` → `config.Server.Features`

## Environment Variable Naming

### Naming Convention

Environment variables follow this pattern:
```
{PREFIX}_{SECTION}_{FIELD}
```

### Key Transformations

1. **Dots to Underscores**: Config keys with dots become underscores
   - `server.host` → `MYAPP_SERVER_HOST`
   - `nv-http-client.timeout` → `MYAPP_NV_HTTP_CLIENT_TIMEOUT`

2. **Hyphens to Underscores**: Hyphens in config keys become underscores
   - `max-idle-conns` → `MAX_IDLE_CONNS`

3. **Uppercase**: All environment variable names are uppercase

### Examples

| Config Key | Environment Variable |
|------------|---------------------|
| `server.host` | `MYAPP_SERVER_HOST` |
| `server.max-connections` | `MYAPP_SERVER_MAX_CONNECTIONS` |
| `database.connection-pool.size` | `MYAPP_DATABASE_CONNECTION_POOL_SIZE` |

## Type-Aware Conversion

The library performs **type-aware conversion** based on the target struct field type. This ensures that environment variable values are converted appropriately without unwanted side effects.

### Supported Types

| Go Type | Environment Variable Format | Example |
|---------|----------------------------|---------|
| `string` | Plain text (commas preserved) | `"value1,value2"` → `"value1,value2"` |
| `[]string` | Comma-separated values | `"value1,value2,value3"` → `[]string{"value1", "value2", "value3"}` |
| `int` | Numeric string | `"8080"` → `8080` |
| `bool` | Boolean string | `"true"` → `true` |
| `time.Duration` | Duration string | `"30s"` → `30 * time.Second` |

### Key Principle: Type Safety

**Only slice fields (`[]string`) are converted from comma-separated strings to slices.**

```go
type Config struct {
    // This field will have commas preserved as a single string
    Description string   `koanf:"description"`
    
    // This field will be converted from comma-separated to slice
    Tags        []string `koanf:"tags"`
}
```

**Environment Variables:**
```bash
MYAPP_DESCRIPTION="A service for development, testing, and production"
MYAPP_TAGS="web,api,production"
```

**Result:**
```go
config.Description = "A service for development, testing, and production"  // String preserved
config.Tags = []string{"web", "api", "production"}                        // Converted to slice
```

## Slice Fields (Lists)

### Syntax

For `[]string` fields, use **comma-separated values**:

```bash
MYAPP_SERVER_FEATURES="cache,security,monitoring"
MYAPP_ALLOWED_HOSTS="host1.example.com,host2.example.com,host3.example.com"
```

### Conversion Rules

1. **Comma Separation**: Values are split on commas
2. **Whitespace Trimming**: Leading/trailing whitespace is removed from each element
3. **Empty Handling**: Empty strings result in empty slices

### Examples

| Environment Variable | Resulting Slice |
|---------------------|-----------------|
| `"a,b,c"` | `[]string{"a", "b", "c"}` |
| `"a, b , c"` | `[]string{"a", "b", "c"}` (whitespace trimmed) |
| `"single"` | `[]string{"single"}` |
| `""` | `[]string{}` (empty slice) |
| `"a,b,"` | `[]string{"a", "b", ""}` (includes empty string) |

### Complex Values

Slice elements can contain complex values:

```bash
# API scopes
MYAPP_OAUTH_SCOPES="api:read,api:write,api:admin"

# URLs
MYAPP_ENDPOINTS="https://api1.example.com,https://api2.example.com"

# File paths
MYAPP_CONFIG_FILES="/etc/app/config.yaml,/etc/app/secrets.yaml"
```

## Templated Environment Variables

For dynamic map keys, the library supports templated environment variables:

### Dynamic Map Configuration

```go
type OAuthTarget struct {
    IssuerURL   string   `koanf:"issuerurl"`
    ClientID    string   `koanf:"clientid"`
    Scopes      []string `koanf:"scopes"`
}

type Config struct {
    Targets map[string]OAuthTarget `koanf:"targets"`
}
```

### Templated Environment Variables

```bash
# Creates config.Targets["self"]
MYAPP_TARGETS_SELF_ISSUERURL="https://auth.example.com"
MYAPP_TARGETS_SELF_CLIENTID="client-123"
MYAPP_TARGETS_SELF_SCOPES="api:read,api:write"

# Creates config.Targets["payment-service"]
MYAPP_TARGETS_PAYMENT_SERVICE_ISSUERURL="https://payment-auth.example.com"
MYAPP_TARGETS_PAYMENT_SERVICE_CLIENTID="payment-client-456"
MYAPP_TARGETS_PAYMENT_SERVICE_SCOPES="payment:process,payment:refund"
```

### Key Extraction

The dynamic key is extracted from the environment variable name:
- `MYAPP_TARGETS_PAYMENT_SERVICE_SCOPES` → key: `payment-service`
- Underscores in the key become hyphens in the config
- The key `PAYMENT_SERVICE` becomes `payment-service`

## Validation

### Type Validation

The library validates environment variables during `Load()`:

```go
err := cm.Load()
if err != nil {
    // Will catch type mismatches like:
    // MYAPP_PORT="not-a-number" for an int field
    // MYAPP_ENABLED="maybe" for a bool field
}
```

### Business Logic Validation

If your struct implements a `Validate()` method, it's called automatically:

```go
type Config struct {
    Endpoints []string `koanf:"endpoints"`
}

func (c *Config) Validate() error {
    if len(c.Endpoints) == 0 {
        return fmt.Errorf("at least one endpoint is required")
    }
    return nil
}
```

### Validation Examples

**Type Validation Errors:**
```bash
MYAPP_PORT="not-a-number"  # Error: cannot parse as int
MYAPP_ENABLED="maybe"      # Error: cannot parse as bool
```

**Business Logic Validation:**
```bash
MYAPP_ENDPOINTS=""  # Error: at least one endpoint is required
```

## Examples

### Complete Configuration Example

```go
type ServerConfig struct {
    Host         string   `koanf:"host"`
    Port         int      `koanf:"port"`
    AllowedHosts []string `koanf:"allowed-hosts"`
    Features     []string `koanf:"features"`
    Description  string   `koanf:"description"`
}

type Config struct {
    Server ServerConfig `koanf:"server"`
}
```

**Environment Variables:**
```bash
# String field - commas preserved
MYAPP_SERVER_DESCRIPTION="A web server for development, testing, and production environments"

# Basic types
MYAPP_SERVER_HOST="0.0.0.0"
MYAPP_SERVER_PORT="8080"

# Slice fields - comma-separated conversion
MYAPP_SERVER_ALLOWED_HOSTS="localhost,127.0.0.1,::1"
MYAPP_SERVER_FEATURES="logging,metrics,tracing,auth"
```

**Resulting Configuration:**
```go
config.Server.Description = "A web server for development, testing, and production environments"
config.Server.Host = "0.0.0.0"
config.Server.Port = 8080
config.Server.AllowedHosts = []string{"localhost", "127.0.0.1", "::1"}
config.Server.Features = []string{"logging", "metrics", "tracing", "auth"}
```

### Library Provider Example

```go
// Library configuration
type HTTPClientConfig struct {
    Endpoints []string          `koanf:"endpoints"`
    Headers   map[string]string `koanf:"headers"`
    Timeout   string            `koanf:"timeout"`
}

// Environment variables
MYAPP_HTTPCLIENT_ENDPOINTS="https://api1.example.com,https://api2.example.com"
MYAPP_HTTPCLIENT_TIMEOUT="30s"
```

### Templated Environment Variables Example

```go
type OAuthConfig struct {
    Targets map[string]OAuthTarget `koanf:"targets"`
}

type OAuthTarget struct {
    IssuerURL string   `koanf:"issuerurl"`
    Scopes    []string `koanf:"scopes"`
}
```

**Environment Variables:**
```bash
# Static configuration
MYAPP_OAUTH_TARGETS_SELF_ISSUERURL="https://auth.internal.com"
MYAPP_OAUTH_TARGETS_SELF_SCOPES="internal:read,internal:write"

# Dynamic configuration - creates new map entry
MYAPP_OAUTH_TARGETS_EXTERNAL_API_ISSUERURL="https://external-auth.example.com"
MYAPP_OAUTH_TARGETS_EXTERNAL_API_SCOPES="external:read,external:process,external:callback"
```

**Resulting Configuration:**
```go
config.OAuth.Targets = map[string]OAuthTarget{
    "self": {
        IssuerURL: "https://auth.internal.com",
        Scopes:    []string{"internal:read", "internal:write"},
    },
    "external-api": {
        IssuerURL: "https://external-auth.example.com", 
        Scopes:    []string{"external:read", "external:process", "external:callback"},
    },
}
```

## Best Practices

### 1. Use Short, Unique Prefixes

Choose short but **unique** prefixes for your applications to minimize environment variable length while avoiding collisions:

```bash
# Good - short and unique to your application/service
MYAPP_SERVER_HOST="localhost"        # Clear app identifier
PAYAPI_DATABASE_URL="postgres://..."  # Payment API service
AUTHSVC_JWT_SECRET="secret"          # Auth service
USRMGR_CACHE_TTL="300s"             # User manager service

# Avoid generic prefixes that will collide
APP_HOST="localhost"     # Too generic - every app might use this
API_PORT="8080"          # Too generic - multiple APIs will collide
SVC_HOST="localhost"     # Too generic - multiple services will collide
DB_URL="postgres://..."  # Too generic - multiple DBs will collide

# Avoid overly long prefixes
PAYMENT_SERVICE_DATABASE_URL="postgres://..."        # Too long
MY_AWESOME_APPLICATION_SERVER_HOST="localhost"       # Too verbose
USER_MANAGEMENT_SERVICE_CACHE_TTL="300s"            # Too long
```

**Prefix Selection Strategy:**
- Use your application/service name abbreviation
- 3-6 characters is ideal
- Make it unique within your deployment environment
- Consider your organization's naming conventions

### 2. Slice Field Naming

Make it clear when a field expects multiple values:
```go
type Config struct {
    // Clear that this expects multiple values
    AllowedHosts []string `koanf:"allowed-hosts"`
    Features     []string `koanf:"features"`
    
    // Clear that this is a single value (even with commas)
    Description  string   `koanf:"description"`
}
```

### 3. Validation

Always implement validation for critical configuration:
```go
func (c *Config) Validate() error {
    if len(c.Endpoints) == 0 {
        return fmt.Errorf("at least one endpoint is required")
    }
    
    for _, endpoint := range c.Endpoints {
        if _, err := url.Parse(endpoint); err != nil {
            return fmt.Errorf("invalid endpoint URL '%s': %w", endpoint, err)
        }
    }
    
    return nil
}
```

### 4. Documentation

Document your environment variables:
```go
type Config struct {
    // Server listening address (default: "localhost")
    Host string `koanf:"host" desc:"Server hostname or IP address"`
    
    // Comma-separated list of allowed origins for CORS
    AllowedOrigins []string `koanf:"allowed-origins" desc:"CORS allowed origins"`
}
```

### 5. Default Values

Provide sensible defaults in your YAML configuration:
```yaml
server:
  host: "localhost"
  port: 8080
  allowed-origins: ["http://localhost:3000"]
  features: ["logging", "metrics"]
```

This ensures the application works even without environment variables while still allowing override via:
```bash
MYAPP_SERVER_FEATURES="logging,metrics,tracing,auth"
```
