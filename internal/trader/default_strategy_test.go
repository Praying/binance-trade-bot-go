package trader

import (
	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/models"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"testing"
)

// MockRestClient is a mock implementation of the RestClientInterface.
type MockRestClient struct {
	mock.Mock
}

func (m *MockRestClient) GetServerTime() (int64, error) {
	args := m.Called()
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockRestClient) GetAllTickerPrices() (map[string]string, error) {
	args := m.Called()
	return args.Get(0).(map[string]string), args.Error(1)
}

func (m *MockRestClient) GetExchangeInfo() (*binance.ExchangeInfoResponse, error) {
	args := m.Called()
	return args.Get(0).(*binance.ExchangeInfoResponse), args.Error(1)
}

func (m *MockRestClient) CreateOrder(symbol, side string, quantity float64) (*binance.CreateOrderResponse, error) {
	args := m.Called(symbol, side, quantity)
	return args.Get(0).(*binance.CreateOrderResponse), args.Error(1)
}

// setupTest creates a full test environment with a mock client and in-memory DB.
func setupTest(t *testing.T) (*gorm.DB, *MockRestClient) {
	// Use a new, non-shared in-memory database for each test to ensure isolation.
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	assert.NoError(t, err)

	err = db.AutoMigrate(&models.Coin{}, &models.Pair{})
	assert.NoError(t, err)

	mockClient := new(MockRestClient)

	return db, mockClient
}

func TestDefaultStrategy_Scout_NoOpportunity(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 16.0, MinQty: 0.01})

	strategy := DefaultStrategy{lastUsedCoinSymbol: "BTC"}
	ctx := StrategyContext{
		Logger:     zap.NewNop(),
		Cfg:        &config.Config{},
		RestClient: mockClient,
		DB:         db,
	}

	// Expect a call to get prices, but return prices that are not profitable
	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "3800", // Ratio = 15.78, not profitable vs 16.0
	}, nil)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.NoError(t, err)
	mockClient.AssertExpectations(t)
	// We assert that CreateOrder was NOT called. The mock library handles this automatically.
}

func TestDefaultStrategy_Scout_PriceFetchError(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0})

	strategy := DefaultStrategy{lastUsedCoinSymbol: "BTC"}
	ctx := StrategyContext{
		Logger:     zap.NewNop(),
		RestClient: mockClient,
		DB:         db,
	}

	// Expect a call to get prices, and return an error
	mockClient.On("GetAllTickerPrices").Return(map[string]string{}, errors.New("API down"))

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "API down")
	mockClient.AssertExpectations(t)
}

func TestDefaultStrategy_Scout_ProfitableTrade_Success(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0, MinQty: 0.01})

	strategy := DefaultStrategy{lastUsedCoinSymbol: "BTC"}
	ctx := StrategyContext{
		Logger: zap.NewNop(),
		Cfg: &config.Config{
			Trading: config.Trading{
				Quantity: 1.0,
				Bridge:   "USDT",
				DryRun:   false,
			},
		},
		RestClient: mockClient,
		DB:         db,
		// Mock exchange rules to allow for quantity formatting
		ExchangeRules: map[string]binance.SymbolInfo{
			"BTCUSDT": {
				Filters: []binance.Filter{
					{FilterType: "LOT_SIZE", StepSize: "0.00001", MinQty: "0.00001"},
				},
			},
			"ETHUSDT": {
				Filters: []binance.Filter{
					{FilterType: "LOT_SIZE", StepSize: "0.01", MinQty: "0.001"},
				},
			},
		},
	}

	// Expect a call to get prices, and return prices that ARE profitable
	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "3900", // Ratio = 15.38, profitable vs 15.0, should trigger a trade
	}, nil)

	// Expect a call to create an order
	// With a quantity of 1.0 BTC, we expect to buy 14.63 ETH (current ratio)
	// Expect the two-step jump:
	// 1. Sell BTC for USDT
	mockClient.On("CreateOrder", "BTCUSDT", "SELL", 1.0).Return(&binance.CreateOrderResponse{OrderID: 1}, nil)
	// 2. Buy ETH with USDT
	//    - We need to calculate the expected buy quantity:
	//      1.0 BTC * 60000 USDT/BTC = 60000 USDT
	//      60000 USDT / 4100 USDT/ETH = 14.634... ETH
	//    - The formatQuantity will floor this based on the step size "0.01"
	mockClient.On("CreateOrder", "ETHUSDT", "BUY", 15.38).Return(&binance.CreateOrderResponse{OrderID: 2}, nil)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.NoError(t, err)
	mockClient.AssertExpectations(t) // Verifies that CreateOrder was called
}

func TestDefaultStrategy_Scout_ProfitableTrade_OrderFails(t *testing.T) {
	// Arrange
	db, mockClient := setupTest(t)
	db.Create(&models.Coin{Symbol: "BTC", Quantity: 1.0})
	db.Create(&models.Pair{FromCoinSymbol: "BTC", ToCoinSymbol: "ETH", Ratio: 15.0, MinQty: 0.01})

	strategy := DefaultStrategy{lastUsedCoinSymbol: "BTC"}
	ctx := StrategyContext{
		Logger: zap.NewNop(),
		Cfg: &config.Config{
			Trading: config.Trading{
				Quantity: 1.0,
				Bridge:   "USDT",
				DryRun:   false,
			},
		},
		RestClient: mockClient,
		DB:         db,
		ExchangeRules: map[string]binance.SymbolInfo{
			"BTCUSDT": {
				Filters: []binance.Filter{
					{FilterType: "LOT_SIZE", StepSize: "0.00001", MinQty: "0.00001"},
				},
			},
			"ETHUSDT": {
				Filters: []binance.Filter{
					{FilterType: "LOT_SIZE", StepSize: "0.01", MinQty: "0.001"},
				},
			},
		},
	}

	mockClient.On("GetAllTickerPrices").Return(map[string]string{
		"BTCUSDT": "60000",
		"ETHUSDT": "3900",
	}, nil)

	// Expect a call to create an order, but it fails
	// Expect the first step (SELL BTC) to succeed
	mockClient.On("CreateOrder", "BTCUSDT", "SELL", 1.0).Return(&binance.CreateOrderResponse{OrderID: 1}, nil)
	// Expect the second step (BUY ETH) to fail
	mockClient.On("CreateOrder", "ETHUSDT", "BUY", 15.38).Return(
		&binance.CreateOrderResponse{},
		errors.New("insufficient funds"),
	)

	// Act
	err := strategy.Scout(ctx)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient funds")
	mockClient.AssertExpectations(t)
}
