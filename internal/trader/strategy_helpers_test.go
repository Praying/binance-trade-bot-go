package trader

import (
	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/models"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestCalculateProfitForPair(t *testing.T) {
	// Setup a mock context for testing
	mockCtx := StrategyContext{
		Logger: zap.NewNop(), // Use a no-op logger for tests
		Cfg: &config.Config{
			Trading: config.Trading{
				Bridge:      "USDT",
				FeeRate:     0.001, // 0.1%
				ScoutMargin: 0.05,  // 0.05%
			},
		},
	}

	// Test cases
	testCases := []struct {
		name           string
		pair           models.Pair
		prices         map[string]string
		expectedProfit float64
		expectError    bool
	}{
		{
			name: "Profitable Jump",
			pair: models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0}, // Old ratio was 15
			prices: map[string]string{
				"BTCUSDT": "30000",
				"ETHUSDT": "1800", // New ratio is 30000/1800 = 16.67
			},
			// Profit = (16.67 * (1-0.001)^2) / 15 - 1 - 0.0005 ~= 10.9%
			expectedProfit: 0.1091,
			expectError:    false,
		},
		{
			name: "Unprofitable Jump",
			pair: models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 17.0}, // Old ratio was 17
			prices: map[string]string{
				"BTCUSDT": "30000",
				"ETHUSDT": "1800", // New ratio is 16.67
			},
			// Profit = (16.67 * (1-0.001)^2) / 17 - 1 - 0.0005 ~= -2.2%
			expectedProfit: -0.0220,
			expectError:    false,
		},
		{
			name: "Jump to Bridge",
			pair: models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "USDT"},
			prices: map[string]string{
				"BTCUSDT": "30000",
			},
			expectedProfit: 0,
			expectError:    false,
		},
		{
			name: "Price data missing for FromCoin",
			pair: models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH"},
			prices: map[string]string{
				"ETHUSDT": "1800",
			},
			expectError: true,
		},
		{
			name: "Price data missing for ToCoin",
			pair: models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH"},
			prices: map[string]string{
				"BTCUSDT": "30000",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profit, err := calculateProfitForPair(mockCtx, &tc.pair, tc.prices)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Use a small tolerance for float comparison
				assert.InDelta(t, tc.expectedProfit, profit, 0.001)
			}
		})
	}
}

func TestFormatQuantity(t *testing.T) {
	// Using JSON unmarshaling to bypass direct type reference issues.
	exchangeRulesJSON := `{
		"BTCUSDT": {
			"symbol": "BTCUSDT",
			"filters": [
				{
					"filterType": "LOT_SIZE",
					"stepSize": "0.00001000",
					"minQty": "0.00001000"
				}
			]
		}
	}`
	var exchangeRules map[string]binance.SymbolInfo
	err := json.Unmarshal([]byte(exchangeRulesJSON), &exchangeRules)
	assert.NoError(t, err)

	mockCtx := StrategyContext{
		Logger:        zap.NewNop(),
		ExchangeRules: exchangeRules,
	}

	testCases := []struct {
		name        string
		symbol      string
		quantity    float64
		expectedQty float64
		expectError bool
	}{
		{
			name:        "Quantity needs flooring",
			symbol:      "BTCUSDT",
			quantity:    1.23456789,
			expectedQty: 1.23456,
			expectError: false,
		},
		{
			name:        "Quantity is already correct",
			symbol:      "BTCUSDT",
			quantity:    1.23456,
			expectedQty: 1.23456,
			expectError: false,
		},
		{
			name:        "Quantity is below MinQty",
			symbol:      "BTCUSDT",
			quantity:    0.000001,
			expectError: true,
		},
		{
			name:        "Symbol has no specific rule",
			symbol:      "ETHUSDT", // This rule doesn't exist in our mock
			quantity:    1.23456789,
			expectedQty: 1.23456789, // Should return original value
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			formattedQty, err := formatQuantity(mockCtx, tc.symbol, tc.quantity)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedQty, formattedQty)
			}
		})
	}
}
