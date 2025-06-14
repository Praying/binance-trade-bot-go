package trader

import (
	"context"
	"time"

	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Engine is the core trading engine that runs a given strategy.
type Engine struct {
	logger     *zap.Logger
	cfg        *config.Config
	db         *gorm.DB
	restClient *binance.RestClient
	strategy   Strategy
}

// NewEngine creates a new trading engine with a specific strategy.
func NewEngine(logger *zap.Logger, cfg *config.Config, restClient *binance.RestClient, db *gorm.DB, strategy Strategy) *Engine {
	return &Engine{
		logger:     logger,
		cfg:        cfg,
		db:         db,
		restClient: restClient,
		strategy:   strategy,
	}
}

// Run starts the trading engine's main loop.
func (e *Engine) Run(ctx context.Context) {
	e.logger.Info("Initializing trading strategy...", zap.String("strategy", e.strategy.Name()))

	// Fetch and cache exchange info
	e.logger.Info("Fetching exchange information...")
	info, err := e.restClient.GetExchangeInfo()
	if err != nil {
		e.logger.Fatal("could not get exchange info", zap.Error(err))
	}
	exchangeRules := make(map[string]binance.SymbolInfo)
	for _, s := range info.Symbols {
		exchangeRules[s.Symbol] = s
	}
	e.logger.Info("Successfully cached exchange information", zap.Int("count", len(exchangeRules)))

	// Create the context for the strategy
	strategyCtx := StrategyContext{
		Logger:        e.logger,
		Cfg:           e.cfg,
		RestClient:    e.restClient,
		DB:            e.db,
		ExchangeRules: exchangeRules,
	}

	if err := e.strategy.Initialize(strategyCtx); err != nil {
		e.logger.Fatal("Failed to initialize strategy", zap.Error(err))
	}
	e.logger.Info("Strategy initialized successfully.")

	// Using the interval from config for the ticker.
	interval := time.Duration(e.cfg.Trading.TickInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.logger.Info("Starting scout loop", zap.String("strategy", e.strategy.Name()), zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("Stopping trading engine...")
			return
		case <-ticker.C:
			if err := e.strategy.Scout(strategyCtx); err != nil {
				e.logger.Error("Strategy scout failed", zap.Error(err), zap.String("strategy", e.strategy.Name()))
			}
		}
	}
}
