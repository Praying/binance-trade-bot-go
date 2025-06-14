package trader

import (
	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// StrategyContext provides the strategy with access to the core components.
type StrategyContext struct {
	Logger        *zap.Logger
	Cfg           *config.Config
	RestClient    binance.RestClientInterface
	DB            *gorm.DB
	ExchangeRules map[string]binance.SymbolInfo
}

// Strategy defines the interface for a trading strategy.
type Strategy interface {
	// Name returns the unique name of the strategy.
	Name() string

	// Initialize gives the strategy a chance to perform setup tasks.
	Initialize(ctx StrategyContext) error

	// Scout is the main logic of the strategy, called periodically by the engine.
	Scout(ctx StrategyContext) error
}
