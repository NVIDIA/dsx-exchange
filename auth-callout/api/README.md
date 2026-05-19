# API Tests

This directory contains API test specifications that verify the shape of the API and provide intuitive interfaces for testing it.

[[_TOC_]]

## VSCode REST Client

The `.http` files contain [VSCode REST Client](https://marketplace.visualstudio.com/items?itemName=humao.rest-client) specs for testing the API endpoints.

This is a form of executable documentation that can be used to invoke API calls in arbitrary combinations and orders.

## How To

### Configuration

Copy `env.example` in this directory to `.env` and populate with the required secrets for your testing.

For local/devspace development and testing, the example contents are valid for the necessary operations.

### Usage

First click a route that returns a token, then click an authenticated route for your service.

**Available test files:**
- `basic-auth-callout.http` - Simplified version with common endpoints
- `comprehensive-auth-callout.http` - Exhaustive list of `Keycloak` and `SSA` tokens and routes
- `metrics.http` - Metrics endpoint verification and Prometheus queries
