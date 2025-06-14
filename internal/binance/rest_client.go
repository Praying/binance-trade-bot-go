package binance

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"binance-trade-bot-go/internal/config"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

const (
	baseURL         = "https://api.binance.com/api/v3"
	testnetBaseURL  = "https://testnet.binance.vision/api/v3"
	recvWindow      = "5000" // How long a request is valid in milliseconds
	OrderTypeMarket = "MARKET"
	OrderSideBuy    = "BUY"
	OrderSideSell   = "SELL"
)

// RestClientInterface defines the interface for the Binance REST API client.
type RestClientInterface interface {
	GetServerTime() (int64, error)
	GetAllTickerPrices() (map[string]string, error)
	GetExchangeInfo() (*ExchangeInfoResponse, error)
	CreateOrder(symbol, side string, quantity float64) (*CreateOrderResponse, error)
}

// RestClient is a client for the Binance REST API.
// It implements the RestClientInterface.
type RestClient struct {
	client    *resty.Client
	apiKey    string
	secretKey string
	logger    *zap.Logger
	limiter   *rate.Limiter
}

// ensure RestClient implements the interface
var _ RestClientInterface = (*RestClient)(nil)

// NewRestClient creates a new Binance REST API client.
func NewRestClient(cfg *config.Binance, logger *zap.Logger) *RestClient {
	var url string
	if cfg.Testnet {
		url = testnetBaseURL
		logger.Warn("Using Binance Testnet")
	} else {
		url = baseURL
		logger.Info("Using Binance Production API")
	}

	client := resty.New().SetBaseURL(url)

	// Initialize the rate limiter
	// rate.Limit is requests per second.
	limiter := rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateLimitBurst)

	return &RestClient{
		client:    client,
		apiKey:    cfg.ApiKey,
		secretKey: cfg.SecretKey,
		logger:    logger,
		limiter:   limiter,
	}
}

// sign creates a HMAC-SHA256 signature for the request.
func (c *RestClient) sign(data string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

// GetServerTime fetches the current server time from Binance.
// This is a good endpoint to test connectivity.
func (c *RestClient) GetServerTime() (int64, error) {
	type ServerTimeResponse struct {
		ServerTime int64 `json:"serverTime"`
	}

	req := c.client.R().
		SetResult(&ServerTimeResponse{})
	ctx := context.Background()

	resp, err := c.doRequest(ctx, "GET", "/time", req)
	if err != nil {
		c.logger.Error("Failed to get server time", zap.Error(err))
		return 0, fmt.Errorf("failed to get server time: %w", err)
	}

	result := resp.Result().(*ServerTimeResponse)
	return result.ServerTime, nil
}

// TickerPrice represents the response for a single ticker price.
type TickerPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// doRequest handles the actual request execution with rate limiting and retry logic.
func (c *RestClient) doRequest(ctx context.Context, method, url string, req *resty.Request) (*resty.Response, error) {
	var resp *resty.Response
	var err error
	const maxRetries = 3

	for i := 0; i < maxRetries; i++ {
		// Wait for the rate limiter
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("rate limiter wait failed: %w", err)
		}

		c.logger.Debug("Executing request", zap.String("method", method), zap.String("url", c.client.BaseURL+url))
		resp, err = req.Execute(method, url)

		if err == nil && !resp.IsError() {
			return resp, nil // Success
		}

		// Analyze error and decide whether to retry
		shouldRetry := false
		var retryAfter time.Duration

		if resp != nil {
			statusCode := resp.StatusCode()
			if statusCode == http.StatusTooManyRequests || statusCode == 418 { // HTTP 429 or 418
				shouldRetry = true
				retryAfterHeader := resp.Header().Get("Retry-After")
				if seconds, err := strconv.Atoi(retryAfterHeader); err == nil {
					retryAfter = time.Duration(seconds) * time.Second
				}
			} else if statusCode >= 500 { // Server errors
				shouldRetry = true
			}
		} else { // Network or other client-side errors
			shouldRetry = true
		}

		if !shouldRetry {
			return nil, fmt.Errorf("request failed with status %s: %s", resp.Status(), resp.String())
		}

		// If we should retry, calculate wait time
		if retryAfter == 0 {
			// Exponential backoff: 1s, 2s, 4s
			retryAfter = time.Duration(math.Pow(2, float64(i))) * time.Second
		}

		c.logger.Warn("Request failed, retrying...",
			zap.Int("attempt", i+1),
			zap.Duration("retry_after", retryAfter),
			zap.Error(err),
		)

		select {
		case <-time.After(retryAfter):
			continue
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries, err)
}

// GetAllTickerPrices fetches the latest price for all symbols.
func (c *RestClient) GetAllTickerPrices() (map[string]string, error) {
	var prices []*TickerPrice

	req := c.client.R().
		SetResult(&prices).
		SetHeader("Content-Type", "application/json")
	ctx := context.Background()

	resp, err := c.doRequest(ctx, "GET", "/ticker/price", req)
	if err != nil {
		return nil, fmt.Errorf("failed to get all ticker prices: %w", err)
	}

	result := resp.Result().(*[]*TickerPrice)
	priceMap := make(map[string]string, len(*result))
	for _, p := range *result {
		priceMap[p.Symbol] = p.Price
	}

	return priceMap, nil
}

// ExchangeInfoResponse represents the full response from the /exchangeInfo endpoint.
type ExchangeInfoResponse struct {
	Symbols []SymbolInfo `json:"symbols"`
}

// SymbolInfo contains information about a specific trading symbol.
type SymbolInfo struct {
	Symbol  string   `json:"symbol"`
	Status  string   `json:"status"`
	Filters []Filter `json:"filters"`
}

// Filter represents a single filter for a symbol.
// We are interested in the LOT_SIZE filter to get the stepSize.
type Filter struct {
	FilterType string `json:"filterType"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
}

// GetExchangeInfo fetches exchange trading rules and symbol information.
func (c *RestClient) GetExchangeInfo() (*ExchangeInfoResponse, error) {
	var exchangeInfo ExchangeInfoResponse

	req := c.client.R().
		SetResult(&exchangeInfo).
		SetHeader("Content-Type", "application/json")
	ctx := context.Background()

	resp, err := c.doRequest(ctx, "GET", "/exchangeInfo", req)
	if err != nil {
		return nil, fmt.Errorf("failed to get exchange info: %w", err)
	}

	return resp.Result().(*ExchangeInfoResponse), nil
}

// CreateOrderResponse represents the response from creating a new order.
type CreateOrderResponse struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	ClientOrderID       string `json:"clientOrderId"`
	TransactTime        int64  `json:"transactTime"`
	Price               string `json:"price"`
	OrigQuantity        string `json:"origQty"`
	ExecutedQuantity    string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status              string `json:"status"`
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`
	Side                string `json:"side"`
}

// CreateOrder places a new order on Binance.
// For simplicity, this example creates a MARKET order.
func (c *RestClient) CreateOrder(symbol, side string, quantity float64) (*CreateOrderResponse, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("type", OrderTypeMarket)
	params.Set("quantity", fmt.Sprintf("%f", quantity))
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	params.Set("recvWindow", recvWindow)

	queryString := params.Encode()
	signature := c.sign(queryString)
	params.Set("signature", signature)

	req := c.client.R().
		SetHeader("X-MBX-APIKEY", c.apiKey).
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		SetBody(params.Encode()).
		SetResult(&CreateOrderResponse{})

	ctx := context.Background()

	resp, err := c.doRequest(ctx, "POST", "/order", req)
	if err != nil {
		c.logger.Error("Failed to create order after multiple attempts",
			zap.Error(err),
			zap.String("symbol", symbol),
		)
		return nil, fmt.Errorf("failed to create order: %w", err)
	}

	result := resp.Result().(*CreateOrderResponse)
	c.logger.Info("Successfully created order", zap.Any("order", result))
	return result, nil
}
