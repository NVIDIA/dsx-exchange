package apitests

import (
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// APITestSuite represents the test suite for the web service API
type APITestSuite struct {
	suite.Suite
	baseURL    string
	httpClient *http.Client
}

// SetupSuite runs before the test suite and initializes the test environment.
// It sets up the base URL from environment variables, creates an HTTP client with timeout,
// and waits for the service to be available before running tests.
func (suite *APITestSuite) SetupSuite() {
	if testing.Short() {
		suite.T().Skip("skipping integration tests in short mode")
	}
	// Get service URL from environment variable, default to localhost
	serviceURL := os.Getenv("AUTH_CALLOUT_SERVICE_URL")
	if serviceURL == "" {
		serviceURL = "http://localhost:8000"
	}
	suite.baseURL = serviceURL

	suite.httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	// Wait for service to be ready
	suite.waitForService()
}

// waitForService waits for the service to be available by polling the /healthz endpoint.
// It retries up to 30 times with 1-second intervals. Fails the test suite if the service
// is not available after all retries.
func (suite *APITestSuite) waitForService() {
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := suite.httpClient.Get(suite.baseURL + "/healthz")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(1 * time.Second)
	}
	suite.T().Fatal("Service not available after waiting")
}

// makeRequest is a helper method to make HTTP requests to the service.
// Returns the HTTP response, response body as a string, and any error that occurred.
func (suite *APITestSuite) makeRequest(method, path string) (*http.Response, string, error) {
	url := suite.baseURL + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, "", err
	}

	resp, err := suite.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return resp, "", err
	}

	return resp, string(body), nil
}

// TestHealthEndpoint tests the health check endpoint at /healthz.
// Verifies that it returns HTTP 200 OK with "OK" body.
func (suite *APITestSuite) TestHealthEndpoint() {
	resp, body, err := suite.makeRequest("GET", "/healthz")
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), resp)

	assert.Equal(suite.T(), http.StatusOK, resp.StatusCode)
	assert.Equal(suite.T(), "OK", body)
}

// TestNotFoundEndpoint tests that non-existent endpoints return HTTP 404 Not Found.
// Note: Go's http.ServeMux serves the root handler for unmatched paths.
func (suite *APITestSuite) TestNotFoundEndpoint() {
	resp, _, err := suite.makeRequest("GET", "/nonexistent")
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), resp)

	// Go's ServeMux serves the root handler for unmatched paths
	assert.Equal(suite.T(), http.StatusNotFound, resp.StatusCode)
}

// TestAPISuite runs the API test suite using the testify suite framework.
// This is the entry point for running all integration tests.
func TestAPISuite(t *testing.T) {
	suite.Run(t, new(APITestSuite))
}
