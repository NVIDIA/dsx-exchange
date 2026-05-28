<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# Hot Config Reload Usage Examples

## Overview

This document provides practical examples of how to use the hot configuration reload functionality in various scenarios. Each example shows the configuration setup, file structure, and component implementation patterns.

## Scenario 1: Vault Secret Rotation

### Use Case
Vault periodically rotates database passwords and API keys, writing them to a hot config file. The application must automatically pick up these new secrets without restart.

### Setup
```bash
# Application startup
myapp --config prod.yaml --hot-config /vault/secrets.yaml
```

### Configuration Files

**prod.yaml** (static configuration):
```yaml
database:
  host: "prod-db.company.com"
  port: 5432
  database: "myapp"
  max_connections: 20

api:
  endpoint: "https://api.company.com"
  timeout: "30s"
```

**secrets.yaml** (hot config managed by Vault):
```yaml
database:
  password: "vault_rotated_password_v123"

api:
  key: "vault_api_key_abc789"
```

### Component Implementation

```go
// Database client with hot reload support
type DatabaseClient struct {
    cm           *ConfigManager
    conn         *sql.DB
    connMutex    sync.RWMutex
    needsReconnect atomic.Bool
}

func (db *DatabaseClient) WatchedSections() []string {
    return []string{"database"}
}

func (db *DatabaseClient) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == "database" {
        log.Info("Database config changed, marking for reconnection")
        db.needsReconnect.Store(true)
    }
    return nil
}

func (db *DatabaseClient) GetConnection() (*sql.DB, error) {
    // Fast path - no reconnect needed
    if !db.needsReconnect.Load() {
        db.connMutex.RLock()
        conn := db.conn
        db.connMutex.RUnlock()
        if conn != nil {
            return conn, nil
        }
    }

    // Reconnect with new credentials
    db.connMutex.Lock()
    defer db.connMutex.Unlock()

    var config DatabaseConfig
    if err := db.cm.UnmarshalSection("database", &config); err != nil {
        return nil, err
    }

    if db.conn != nil {
        db.conn.Close()
    }

    newConn, err := sql.Open("postgres", config.ConnectionString())
    if err != nil {
        return nil, fmt.Errorf("failed to reconnect: %w", err)
    }

    db.conn = newConn
    db.needsReconnect.Store(false)
    log.Info("Successfully reconnected to database with new credentials")

    return newConn, nil
}

// Usage in application
func (s *Service) ProcessRequest(req *Request) error {
    conn, err := s.dbClient.GetConnection()
    if err != nil {
        return err
    }
    // Use connection normally
}
```

## Scenario 2: Feature Flag Management

### Use Case
Product team wants to enable/disable features dynamically without deployments. Feature flags are managed through a separate system that updates a hot config file.

### Setup
```bash
myapp --config app.yaml --hot-config /etc/myapp/features.yaml,/vault/secrets.yaml
```

### Configuration Files

**features.yaml** (hot config managed by feature flag service):
```yaml
features:
  new_recommendation_algorithm: true
  advanced_analytics: false
  beta_ui: true
  maintenance_mode: false

rate_limits:
  api_requests_per_minute: 1000
  bulk_operations_per_hour: 50
```

### Component Implementation

```go
// Feature service that always gets fresh values
type FeatureService struct {
    cm *ConfigManager
}

func (f *FeatureService) IsEnabled(feature string) bool {
    var features FeatureConfig
    if err := f.cm.UnmarshalSection("features", &features); err != nil {
        log.Error("Failed to load features config", "error", err)
        return false // Fail closed
    }

    switch feature {
    case "new_recommendation_algorithm":
        return features.NewRecommendationAlgorithm
    case "advanced_analytics":
        return features.AdvancedAnalytics
    case "beta_ui":
        return features.BetaUI
    case "maintenance_mode":
        return features.MaintenanceMode
    default:
        return false
    }
}

// Rate limiter with hot reload support
type RateLimiter struct {
    cm      *ConfigManager
    limiter *rate.Limiter
    mutex   sync.RWMutex
    needsUpdate atomic.Bool
}

func (r *RateLimiter) WatchedSections() []string {
    return []string{"rate_limits"}
}

func (r *RateLimiter) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == "rate_limits" {
        r.needsUpdate.Store(true)
    }
    return nil
}

func (r *RateLimiter) Allow() bool {
    if r.needsUpdate.Load() {
        r.updateLimiter()
    }

    r.mutex.RLock()
    limiter := r.limiter
    r.mutex.RUnlock()

    return limiter.Allow()
}

func (r *RateLimiter) updateLimiter() {
    var config RateLimitConfig
    if err := r.cm.UnmarshalSection("rate_limits", &config); err != nil {
        return // Keep existing limiter
    }

    r.mutex.Lock()
    r.limiter = rate.NewLimiter(rate.Limit(config.APIRequestsPerMinute/60), config.APIRequestsPerMinute)
    r.needsUpdate.Store(false)
    r.mutex.Unlock()

    log.Info("Updated rate limiter", "rpm", config.APIRequestsPerMinute)
}

// Usage in request handler
func (s *Service) HandleAPIRequest(w http.ResponseWriter, r *http.Request) {
    if !s.rateLimiter.Allow() {
        http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
        return
    }

    if s.features.IsEnabled("maintenance_mode") {
        http.Error(w, "Service under maintenance", http.StatusServiceUnavailable)
        return
    }

    if s.features.IsEnabled("new_recommendation_algorithm") {
        s.handleWithNewAlgorithm(w, r)
    } else {
        s.handleWithOldAlgorithm(w, r)
    }
}
```

## Scenario 3: Multi-Environment Overrides

### Use Case
Different environments (staging, prod, canary) need different configuration overrides. Operations team updates override files to tune performance or troubleshoot issues.

### Setup
```bash
# Staging
myapp --config base.yaml,staging.yaml --hot-config /etc/overrides/staging.yaml

# Production
myapp --config base.yaml,prod.yaml --hot-config /etc/overrides/prod.yaml,/vault/secrets.yaml

# Canary (more frequent monitoring)
myapp --config base.yaml,prod.yaml --hot-config /etc/overrides/canary.yaml --hot-config-interval 5s
```

### Configuration Files

**staging.yaml** (hot config for staging overrides):
```yaml
logging:
  level: "debug"

server:
  timeout: "60s"  # Longer timeout for debugging

database:
  max_connections: 5  # Lower for staging environment

features:
  debug_endpoints: true
```

**prod.yaml** (hot config for production overrides):
```yaml
logging:
  level: "info"

server:
  timeout: "30s"
  read_timeout: "15s"

database:
  max_connections: 50  # Tuned for production load

monitoring:
  metrics_enabled: true
  tracing_sample_rate: 0.1
```

### Component Implementation

```go
// HTTP server with configurable timeouts
type HTTPServer struct {
    cm     *ConfigManager
    server *http.Server
    mutex  sync.RWMutex
    needsRestart atomic.Bool
}

func (h *HTTPServer) WatchedSections() []string {
    return []string{"server"}
}

func (h *HTTPServer) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == "server" {
        log.Info("Server config changed, will restart on next request")
        h.needsRestart.Store(true)
    }
    return nil
}

func (h *HTTPServer) ensureServer() *http.Server {
    if !h.needsRestart.Load() {
        h.mutex.RLock()
        server := h.server
        h.mutex.RUnlock()
        if server != nil {
            return server
        }
    }

    h.mutex.Lock()
    defer h.mutex.Unlock()

    var config ServerConfig
    if err := h.cm.UnmarshalSection("server", &config); err != nil {
        return h.server // Return existing on error
    }

    if h.server != nil {
        // Gracefully shutdown existing server
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        h.server.Shutdown(ctx)
        cancel()
    }

    h.server = &http.Server{
        ReadTimeout:  config.ReadTimeout,
        WriteTimeout: config.Timeout,
        IdleTimeout:  config.IdleTimeout,
    }

    h.needsRestart.Store(false)
    log.Info("Recreated HTTP server with new timeouts",
        "read_timeout", config.ReadTimeout,
        "write_timeout", config.Timeout)

    return h.server
}

// Logger with dynamic level changes
type Logger struct {
    cm    *ConfigManager
    level atomic.Value // stores slog.Level
}

func (l *Logger) WatchedSections() []string {
    return []string{"logging"}
}

func (l *Logger) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == "logging" {
        var config LoggingConfig
        if err := l.cm.UnmarshalSection("logging", &config); err == nil {
            newLevel := parseLogLevel(config.Level)
            l.level.Store(newLevel)
            log.Info("Updated log level", "level", config.Level)
        }
    }
    return nil
}

func (l *Logger) Enabled(level slog.Level) bool {
    currentLevel := l.level.Load().(slog.Level)
    return level >= currentLevel
}
```

## Scenario 4: Microservice Library Integration

### Use Case
Multiple microservices use shared libraries (HTTP client, database, Redis) that need hot config support. Each service can have different configurations for the same libraries.

### Configuration Files

**service-a-secrets.yaml**:
```yaml
nv-http-client:
  external-api:
    api_key: "service_a_key_123"
    timeout: "45s"

redis:
  cache:
    password: "redis_pass_abc"
```

**service-b-secrets.yaml**:
```yaml
nv-http-client:
  external-api:
    api_key: "service_b_key_456"
    timeout: "30s"

database:
  password: "service_b_db_pass"
```

## Scenario 5: Aliased Provider Hot Config

### Use Case
A service needs multiple instances of the same library (e.g., multiple HTTP clients for different APIs) using aliased providers. Each aliased instance needs independent hot config support.

### Setup
```bash
myapp --config base.yaml --hot-config /vault/api-secrets.yaml,/etc/client-overrides.yaml
```

### Service Configuration with Aliases
```go
// Setup with aliased providers
providers := config.ProvidersWithAliases(
    config.ProviderWithAlias{Provider: &httpclient.ConfigProvider{}},                    // "nv-http-client"
    config.ProviderWithAlias{Provider: &httpclient.ConfigProvider{}, Alias: "api-client"},     // "api-client"
    config.ProviderWithAlias{Provider: &httpclient.ConfigProvider{}, Alias: "scraper-client"}, // "scraper-client"
    config.ProviderWithAlias{Provider: &database.ConfigProvider{}},                     // "database"
    config.ProviderWithAlias{Provider: &redis.ConfigProvider{}, Alias: "session-store"}, // "session-store"
)

cm, err := config.NewService(defaultYAML, ServiceConfig{}, "MYAPP", providers, rootCmd,
    config.WithHotConfigFiles("/vault/api-secrets.yaml", "/etc/client-overrides.yaml"))
```

### Hot Config Files

**api-secrets.yaml** (Vault-managed secrets for different clients):
```yaml
# Original HTTP client (nv-http-client)
nv-http-client:
  shared-client:
    api_key: "main_api_key_123"

# Aliased HTTP client for external API
api-client:
  timeout: "30s"
  api_key: "external_api_key_456"
  max_retries: 3

# Aliased HTTP client for scraping
scraper-client:
  timeout: "60s"
  api_key: "scraper_api_key_789"
  user_agent: "MyApp-Scraper/1.0"

# Aliased Redis for session storage
session-store:
  password: "session_redis_pass_abc"
  database: 1
```

**client-overrides.yaml** (Operational overrides):
```yaml
# Override timeouts for performance tuning
api-client:
  timeout: "45s"           # Increased from 30s
  max_idle_connections: 50

scraper-client:
  timeout: "120s"          # Increased for slow endpoints
  rate_limit_per_second: 5

# Override pool settings
nv-http-client:
  shared-client:
    max_idle_connections: 100
    keep_alive_timeout: "90s"
```

### Component Implementation with Aliases

```go
// HTTP client factory that handles aliased providers
type HTTPClientFactory struct {
    cm *ConfigManager
    clients map[string]*ReloadableHTTPClient
}

func NewHTTPClientFactory(cm *ConfigManager) *HTTPClientFactory {
    return &HTTPClientFactory{
        cm:      cm,
        clients: make(map[string]*ReloadableHTTPClient),
    }
}

// Get client by alias name
func (f *HTTPClientFactory) GetClient(alias string) (*http.Client, error) {
    if reloadableClient, exists := f.clients[alias]; exists {
        return reloadableClient.Get()
    }

    // Create new reloadable client for this alias
    reloadableClient := &ReloadableHTTPClient{
        cm:      f.cm,
        section: alias, // Use the alias as section name
    }

    // Register for config changes
    f.cm.RegisterWatcher(reloadableClient)

    f.clients[alias] = reloadableClient
    return reloadableClient.Get()
}

// Reloadable HTTP client for aliased providers
type ReloadableHTTPClient struct {
    cm           *ConfigManager
    section      string // The aliased section name (e.g., "api-client")
    client       *http.Client
    mutex        sync.RWMutex
    needsRecreate atomic.Bool
}

func (c *ReloadableHTTPClient) WatchedSections() []string {
    return []string{c.section} // Watch the aliased section
}

func (c *ReloadableHTTPClient) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == c.section {
        log.Info("HTTP client config changed", "alias", c.section)
        c.needsRecreate.Store(true)
    }
    return nil
}

func (c *ReloadableHTTPClient) Get() (*http.Client, error) {
    if !c.needsRecreate.Load() {
        c.mutex.RLock()
        client := c.client
        c.mutex.RUnlock()
        if client != nil {
            return client, nil
        }
    }

    c.mutex.Lock()
    defer c.mutex.Unlock()

    // Unmarshal config using the aliased section name
    var config HTTPClientConfig
    if err := c.cm.UnmarshalSection(c.section, &config); err != nil {
        return c.client, err // Return old client on error
    }

    // Create new HTTP client with the aliased config
    transport := &http.Transport{
        MaxIdleConns:    config.MaxIdleConnections,
        IdleConnTimeout: config.KeepAliveTimeout,
    }

    c.client = &http.Client{
        Timeout:   config.Timeout,
        Transport: transport,
    }

    c.needsRecreate.Store(false)
    log.Info("Recreated HTTP client with new config", "alias", c.section, "timeout", config.Timeout)

    return c.client, nil
}

// Service usage with multiple aliased clients
type APIService struct {
    httpFactory *HTTPClientFactory
}

func (s *APIService) CallExternalAPI(data interface{}) error {
    // Use the aliased "api-client" configuration
    client, err := s.httpFactory.GetClient("api-client")
    if err != nil {
        return err
    }

    // Make API call with automatically reloading config
    resp, err := client.Post("https://external.api.com/endpoint", "application/json", nil)
    // ... handle response
    return nil
}

func (s *APIService) ScrapeWebsite(url string) error {
    // Use the aliased "scraper-client" configuration
    client, err := s.httpFactory.GetClient("scraper-client")
    if err != nil {
        return err
    }

    // Make scraping request with different timeout/user-agent
    resp, err := client.Get(url)
    // ... handle response
    return nil
}

func (s *APIService) CallSharedService() error {
    // Use the original "nv-http-client.shared-client" configuration
    client, err := s.httpFactory.GetClient("nv-http-client.shared-client")
    if err != nil {
        return err
    }

    // Make internal service call
    resp, err := client.Get("https://internal.service.com/api")
    // ... handle response
    return nil
}
```

### Key Points for Aliased Provider Hot Config

1. **Section Names**: Hot config files must use the aliased section names (e.g., `api-client`, not `httpclient`)

2. **Independent Configuration**: Each aliased provider instance has completely independent configuration

3. **Component Registration**: Components register watchers using the aliased section name

4. **Validation**: Works automatically since aliased providers are registered with their aliased names

5. **Flexibility**: Allows the same library to be configured differently for different use cases within the same service

### Library Implementation

```go
// Enhanced HTTP client library with hot reload
type HTTPClientProvider struct{}

func (p *HTTPClientProvider) CreateClient(cm *ConfigManager, section string) (*HTTPClient, error) {
    client := &HTTPClient{
        cm:      cm,
        section: section,
    }

    // Register for config changes
    cm.RegisterWatcher(client)

    return client, nil
}

type HTTPClient struct {
    cm           *ConfigManager
    section      string
    client       *http.Client
    mutex        sync.RWMutex
    needsRecreate atomic.Bool
}

func (c *HTTPClient) WatchedSections() []string {
    return []string{c.section}
}

func (c *HTTPClient) OnConfigChange(section string, oldValue, newValue interface{}) error {
    if section == c.section {
        c.needsRecreate.Store(true)
        log.Info("HTTP client config changed", "section", section)
    }
    return nil
}

func (c *HTTPClient) Get(url string) (*http.Response, error) {
    client, err := c.getClient()
    if err != nil {
        return nil, err
    }
    return client.Get(url)
}

func (c *HTTPClient) getClient() (*http.Client, error) {
    if !c.needsRecreate.Load() {
        c.mutex.RLock()
        client := c.client
        c.mutex.RUnlock()
        if client != nil {
            return client, nil
        }
    }

    c.mutex.Lock()
    defer c.mutex.Unlock()

    var config HTTPClientConfig
    if err := c.cm.UnmarshalSection(c.section, &config); err != nil {
        return c.client, err // Return old client on error
    }

    c.client = &http.Client{
        Timeout: config.Timeout,
        Transport: &http.Transport{
            MaxIdleConns: config.MaxIdleConns,
        },
    }

    c.needsRecreate.Store(false)
    return c.client, nil
}

// Service usage
func main() {
    providers := []config.LibraryConfigProvider{
        &httpclient.ConfigProvider{},
        &database.ConfigProvider{},
        &redis.ConfigProvider{},
    }

    cm, err := config.NewService(
        defaultYAML,
        ServiceConfig{},
        "SERVICE_A",
        providers,
        rootCmd,
        config.WithHotConfigFiles("/vault/service-a-secrets.yaml"),
    )

    // Create library clients with hot reload support
    httpClient, err := httpclient.CreateClient(cm, "nv-http-client.external-api")
    dbClient, err := database.CreateClient(cm, "database")
    redisClient, err := redis.CreateClient(cm, "redis.cache")

    cm.Load()
    cm.StartWatching()

    // Clients automatically handle config changes
}
```

## Best Practices

### 1. Error Handling
- Always handle config reload errors gracefully
- Provide fallback to previous working configuration
- Log detailed error information for debugging
- Implement health checks to monitor reload status

### 2. Performance Optimization
- Use atomic operations for fast-path checks
- Implement lazy recreation rather than immediate updates
- Cache expensive-to-create resources
- Debounce rapid configuration changes

### 3. Security Considerations
- Validate file permissions on hot config files
- Log all configuration changes for audit
- Consider rate limiting for reload frequency
- Sanitize sensitive values in logs

### 4. Testing Strategies
- Test with simulated file changes
- Verify component notification works correctly
- Test error scenarios (invalid config, file permission issues)
- Load test with frequent configuration changes

### 5. Monitoring and Observability
- Expose metrics for reload frequency and success rate
- Log configuration change events with details
- Provide health check endpoints showing reload status
- Monitor file watcher health and restart if needed
