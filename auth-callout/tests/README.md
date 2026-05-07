# API Integration Tests

This directory contains integration tests for the auth-callout service.

## Running Tests

### Prerequisites

- Go 1.25
- Service running on port 8000

### Local Testing

```bash
# Start the service first
cd ..
make run

# In another terminal, run the tests
cd tests
go test -v
```

### Docker Testing

```bash
# Using Docker Compose
make compose-up
cd tests
AUTH-CALLOUT_SERVICE_URL=http://localhost:8000 go test -v
make compose-down
```

### Environment Variables

- `AUTH-CALLOUT_SERVICE_URL` - Service base URL (default: http://localhost:8000)

## Test Coverage

The tests cover:

- ✅ Root endpoint (`/`) returns "hello, world"
- ✅ Health check endpoint (`/healthz`) returns "OK"
- ✅ Non-existent endpoints return 404
- ✅ Basic service functionality
- ✅ HTTP method handling
