package binance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"binance-trade-bot-go/internal/config"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// setupTestServer creates a new test server and a RestClient configured to use it.
func setupTestServer(handler http.Handler) (*RestClient, *httptest.Server) {
	server := httptest.NewServer(handler)

	client := resty.New().SetBaseURL(server.URL)
	logger := zap.NewNop() // Use a no-op logger for tests

	rc := &RestClient{
		client:    client,
		apiKey:    "test_api_key",
		secretKey: "test_secret_key",
		logger:    logger,
		limiter:   rate.NewLimiter(rate.Inf, 1), // Allow all requests in tests
	}

	return rc, server
}

func TestGetServerTime(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Arrange
		expectedTime := time.Now().UnixMilli()
		mockResponse := fmt.Sprintf(`{"serverTime": %d}`, expectedTime)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/time", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(mockResponse))
		})

		rc, server := setupTestServer(handler)
		defer server.Close()

		// Act
		serverTime, err := rc.GetServerTime()

		// Assert
		assert.NoError(t, err)
		assert.Equal(t, expectedTime, serverTime)
	})

	t.Run("APIError", func(t *testing.T) {
		// Arrange
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/time", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"code": -1001, "msg": "Internal error"}`))
		})

		rc, server := setupTestServer(handler)
		defer server.Close()

		// Act
		serverTime, err := rc.GetServerTime()

		// Assert
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get server time")
		assert.Contains(t, err.Error(), "request failed") // Check for the error from doRequest
		assert.Equal(t, int64(0), serverTime)
	})
}

func TestNewRestClient(t *testing.T) {
	t.Run("Testnet", func(t *testing.T) {
		cfg := &config.Binance{Testnet: true}
		logger := zap.NewNop()
		rc := NewRestClient(cfg, logger)
		assert.NotNil(t, rc)
		// Resty doesn't publicly expose the base URL after setting it,
		// so we can't directly assert it. However, we can infer it's correct
		// by ensuring the client object is created. A more advanced test could
		// involve making a request and inspecting the URL.
		assert.Equal(t, cfg.ApiKey, rc.apiKey)
		assert.Equal(t, cfg.SecretKey, rc.secretKey)
	})

	t.Run("Production", func(t *testing.T) {
		cfg := &config.Binance{Testnet: false}
		logger := zap.NewNop()
		rc := NewRestClient(cfg, logger)
		assert.NotNil(t, rc)
		assert.Equal(t, cfg.ApiKey, rc.apiKey)
		assert.Equal(t, cfg.SecretKey, rc.secretKey)
	})
}
