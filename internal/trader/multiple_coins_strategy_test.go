package trader

import (
	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/models"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestMultipleCoinsStrategy_Scout_NoOpportunity(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0, Enabled: true})
	db.Create(&models.Coin{Symbol: "ETH", Quantity: 15.0, Enabled: true})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0, MinQty: 0.01})
	db.Create(&models.Pair{FromCoinSymbol: "ETH", ToCoinSymbol: "BTC", Ratio: 1.0 / 15.0, MinQty: 0.0001})

	strategy := MultipleCoinsStrategy{}
	ctx := StrategyContext{
		Logger: zap.NewNop(),
		Cfg: &config.Config{
			Trading: config.Trading{
				Bridge: "USDT",
				DryRun: false,
			},
		},
		RestClient: mockClient,
		DB:         db,
	}

	// Expect a call to get prices, but return prices that are not profitable
	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "4000", // Ratio = 15.0, not profitable
	}, nil)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestMultipleCoinsStrategy_Scout_ProfitableTrade_Success(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0, Enabled: true})
	db.Create(&models.Coin{Symbol: "ETH", Quantity: 15.0, Enabled: true})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0, MinQty: 0.01})
	db.Create(&models.Pair{FromCoinSymbol: "ETH", ToCoinSymbol: "BTC", Ratio: 1.0 / 15.0, MinQty: 0.0001})

	strategy := MultipleCoinsStrategy{}
	ctx := StrategyContext{
		Logger: zap.NewNop(),
		Cfg: &config.Config{
			Trading: config.Trading{
				Bridge:   "USDT",
				DryRun:   false,
				Quantity: 1.0, // This will be used by ExecuteJump
			},
		},
		RestClient: mockClient,
		DB:         db,
		ExchangeRules: map[string]binance.SymbolInfo{
			"BTCUSDT": {Filters: []binance.Filter{{FilterType: "LOT_SIZE", StepSize: "0.00001", MinQty: "0.00001"}}},
			"ETHUSDT": {Filters: []binance.Filter{{FilterType: "LOT_SIZE", StepSize: "0.01", MinQty: "0.001"}}},
		},
	}

	// Expect a call to get prices, and return prices that ARE profitable for BTC -> ETH
	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "3900", // Ratio = 15.38, profitable vs 15.0
	}, nil)

	// Expect the two-step jump:
	// 1. Sell BTC for USDT
	mockClient.On("CreateOrder", "BTCUSDT", "SELL", 1.0).Return(&binance.CreateOrderResponse{OrderID: 1}, nil)
	// 2. Buy ETH with USDT
	mockClient.On("CreateOrder", "ETHUSDT", "BUY", 15.38).Return(&binance.CreateOrderResponse{OrderID: 2}, nil)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}

func TestMultipleCoinsStrategy_Scout_SelectsBestOpportunity(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0, Enabled: true})
	db.Create(&models.Coin{Symbol: "ETH", Quantity: 15.0, Enabled: true})
	db.Create(&models.Coin{Symbol: "LTC", Quantity: 200.0, Enabled: true})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0, MinQty: 0.01})
	db.Create(&models.Pair{FromCoinSymbol: "ETH", ToCoinSymbol: "BTC", Ratio: 1.0 / 15.0, MinQty: 0.0001})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "LTC", Ratio: 200.0, MinQty: 0.1})
	db.Create(&models.Pair{FromCoinSymbol: "LTC", ToCoinSymbol: "BTC", Ratio: 1.0 / 200.0, MinQty: 0.00001})

	strategy := MultipleCoinsStrategy{}
	ctx := StrategyContext{
		Logger: zap.NewNop(),
		Cfg: &config.Config{
			Trading: config.Trading{
				Bridge:   "USDT",
				DryRun:   false,
				Quantity: 1.0,
			},
		},
		RestClient: mockClient,
		DB:         db,
		ExchangeRules: map[string]binance.SymbolInfo{
			"BTCUSDT": {Filters: []binance.Filter{{FilterType: "LOT_SIZE", StepSize: "0.00001", MinQty: "0.00001"}}},
			"ETHUSDT": {Filters: []binance.Filter{{FilterType: "LOT_SIZE", StepSize: "0.01", MinQty: "0.001"}}},
			"LTCUSDT": {Filters: []binance.Filter{{FilterType: "LOT_SIZE", StepSize: "0.1", MinQty: "0.1"}}},
		},
	}

	// Both ETH and LTC are profitable, but LTC is MORE profitable.
	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "3950", // Profit: (60000/3950)/15 - 1 = 1.2%
		"LTCUSDT": "290",  // Profit: (60000/290)/200 - 1 = 3.4%
	}, nil)

	// Expect a jump to LTC, not ETH
	mockClient.On("CreateOrder", "BTCUSDT", "SELL", 1.0).Return(&binance.CreateOrderResponse{OrderID: 1}, nil)
	mockClient.On("CreateOrder", "LTCUSDT", "BUY", 206.8).Return(&binance.CreateOrderResponse{OrderID: 2}, nil)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
}
