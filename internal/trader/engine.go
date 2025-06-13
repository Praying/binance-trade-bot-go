package trader

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Engine is the core trading engine that runs the polling-based scouting strategy.
type Engine struct {
	logger        *zap.Logger
	cfg           *config.Config
	restClient    *binance.RestClient
	db            *gorm.DB
	exchangeRules map[string]binance.SymbolInfo
}

// NewEngine creates a new trading engine.
func NewEngine(logger *zap.Logger, cfg *config.Config, restClient *binance.RestClient, db *gorm.DB) *Engine {
	return &Engine{
		logger:        logger,
		cfg:           cfg,
		restClient:    restClient,
		db:            db,
		exchangeRules: make(map[string]binance.SymbolInfo),
	}
}

// Run starts the trading engine's main loop.
func (e *Engine) Run(ctx context.Context) {
	e.logger.Info("Initializing trading engine...")
	if err := e.initialize(); err != nil {
		e.logger.Fatal("Failed to initialize engine", zap.Error(err))
	}
	e.logger.Info("Engine initialized successfully.")

	// Using the interval from config for the ticker.
	interval := time.Duration(e.cfg.Trading.TickInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.logger.Info("Starting scout loop", zap.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("Stopping trading engine...")
			return
		case <-ticker.C:
			if err := e.scout(); err != nil {
				e.logger.Error("Scout failed", zap.Error(err))
			}
		}
	}
}

// initialize sets up the initial state, like populating pairs and calculating initial ratios.
func (e *Engine) initialize() error {
	// 0. Fetch and cache exchange info
	e.logger.Info("Fetching exchange information...")
	info, err := e.restClient.GetExchangeInfo()
	if err != nil {
		return fmt.Errorf("could not get exchange info: %w", err)
	}
	for _, s := range info.Symbols {
		e.exchangeRules[s.Symbol] = s
	}
	e.logger.Info("Successfully cached exchange information for symbols", zap.Int("count", len(e.exchangeRules)))

	bridge := e.cfg.Trading.Bridge
	tradeCoins := e.cfg.Trading.TradePairs

	// 1. Populate pairs based on the config trade_pairs against the bridge currency.
	e.logger.Info("Populating trading pairs against bridge currency", zap.String("bridge", bridge))
	for _, coin := range tradeCoins {
		if coin == bridge {
			continue // Don't create a pair for the bridge against itself.
		}
		pair := models.Pair{
			FromCoinSymbol: coin,
			ToCoinSymbol:   bridge,
		}
		// Create pair if it doesn't exist
		if err := e.db.FirstOrCreate(&pair, pair).Error; err != nil {
			return fmt.Errorf("failed to create pair %s/%s: %w", coin, bridge, err)
		}
	}

	// 2. Set initial coin if not present (defaults to the bridge currency).
	var currentCoin models.CurrentCoin
	if err := e.db.First(&currentCoin).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		e.logger.Info("No current coin found, setting initial coin to bridge currency", zap.String("coin", bridge))
		currentCoin = models.CurrentCoin{Symbol: bridge}
		if err := e.db.Create(&currentCoin).Error; err != nil {
			return fmt.Errorf("failed to set initial coin: %w", err)
		}
	}

	// 3. Initialize trade ratios for pairs that don't have one.
	// The ratio represents FromCoin/ToCoin. For a BTC/USDT pair, ratio is Price(BTC)/Price(USDT).
	e.logger.Info("Initializing trade ratios...")
	var pairsToUpdate []models.Pair
	if err := e.db.Where("ratio = ?", 0).Find(&pairsToUpdate).Error; err != nil {
		return fmt.Errorf("failed to find pairs to update: %w", err)
	}

	if len(pairsToUpdate) > 0 {
		e.logger.Info("Found pairs that need ratio initialization", zap.Int("count", len(pairsToUpdate)))
		prices, err := e.restClient.GetAllTickerPrices()
		if err != nil {
			return fmt.Errorf("could not get prices for ratio initialization: %w", err)
		}

		for _, pair := range pairsToUpdate {
			// Since we pair everything against the bridge, the symbol is FromCoin+BridgeCoin
			// e.g., for FromCoin "BTC" and ToCoin "USDT", the symbol is "BTCUSDT"
			symbol := pair.FromCoinSymbol + pair.ToCoinSymbol
			priceStr, ok := prices[symbol]
			if !ok {
				e.logger.Warn("Could not find ticker price for symbol to initialize ratio",
					zap.String("symbol", symbol))
				continue
			}
			price, err := strconv.ParseFloat(priceStr, 64)
			if err != nil {
				e.logger.Error("Failed to parse price for ratio initialization",
					zap.String("symbol", symbol), zap.String("price", priceStr), zap.Error(err))
				continue
			}

			if price > 0 {
				pair.Ratio = price
				if err := e.db.Save(&pair).Error; err != nil {
					e.logger.Error("Failed to save initialized ratio", zap.String("symbol", symbol), zap.Error(err))
				} else {
					e.logger.Info("Initialized ratio for pair", zap.String("symbol", symbol), zap.Float64("ratio", price))
				}
			}
		}
	}
	return nil
}

type tradeOpportunity struct {
	Pair   models.Pair
	Profit float64
}

// scout performs a single round of scouting for profitable trades.
func (e *Engine) scout() error {
	e.logger.Info("Scouting for trades...")

	// 1. Get current held coin
	var currentCoin models.CurrentCoin
	if err := e.db.First(&currentCoin).Error; err != nil {
		return fmt.Errorf("could not get current coin from db: %w", err)
	}
	e.logger.Info("Currently holding coin", zap.String("symbol", currentCoin.Symbol))

	// 2. Get all ticker prices
	prices, err := e.restClient.GetAllTickerPrices()
	if err != nil {
		return fmt.Errorf("could not get all ticker prices: %w", err)
	}

	bridge := e.cfg.Trading.Bridge
	feeRate := e.cfg.Trading.FeeRate

	// 3. & 4. Find the best trade opportunity
	if currentCoin.Symbol == bridge {
		// We hold the bridge currency, so we're looking to BUY a trade coin.
		// This part is concurrent to speed up checking many pairs.
		e.logger.Info("Looking for a coin to BUY with bridge currency")
		var pairs []models.Pair
		e.db.Find(&pairs)

		var wg sync.WaitGroup
		opportunities := make(chan tradeOpportunity, len(pairs))

		for _, p := range pairs {
			wg.Add(1)
			go func(pair models.Pair) {
				defer wg.Done()
				symbol := pair.FromCoinSymbol + pair.ToCoinSymbol // e.g., BTCUSDT
				currentPriceStr, ok := prices[symbol]
				if !ok {
					return
				}
				currentPrice, _ := strconv.ParseFloat(currentPriceStr, 64)

				effectivePrice := currentPrice * (1 + feeRate)
				if currentPrice > 0 && effectivePrice < pair.Ratio {
					profit := (pair.Ratio - effectivePrice) / pair.Ratio
					e.logger.Debug("Potential BUY opportunity",
						zap.String("symbol", symbol),
						zap.Float64("profit_margin", profit),
					)
					opportunities <- tradeOpportunity{Pair: pair, Profit: profit}
				}
			}(p)
		}

		// Wait for all goroutines to finish, then close the channel
		go func() {
			wg.Wait()
			close(opportunities)
		}()
		
		var bestOpp *tradeOpportunity
		for opp := range opportunities {
			if bestOpp == nil || opp.Profit > bestOpp.Profit {
				// We need to copy opp because the loop var is reused.
				currentOpp := opp
				bestOpp = &currentOpp
			}
		}

		if bestOpp != nil {
			e.logger.Info("Found best BUY opportunity",
				zap.String("coin_to_buy", bestOpp.Pair.FromCoinSymbol),
				zap.Float64("profit_margin", bestOpp.Profit))
			e.executeTrade(&bestOpp.Pair, binance.OrderSideBuy)
		} else {
			e.logger.Info("No profitable BUY opportunities found in this cycle.")
		}

	} else {
		// We hold a trade coin, so we're looking to SELL it for the bridge currency.
		// This part is simpler and not concurrent as we only check one pair.
		e.logger.Info("Looking to SELL current coin for bridge currency", zap.String("coin", currentCoin.Symbol))
		pair := models.Pair{}
		e.db.First(&pair, "from_coin_symbol = ? AND to_coin_symbol = ?", currentCoin.Symbol, bridge)

		if pair.ID == 0 {
			return fmt.Errorf("could not find pair for current coin %s", currentCoin.Symbol)
		}

		symbol := pair.FromCoinSymbol + pair.ToCoinSymbol // e.g. BTCUSDT
		currentPriceStr, ok := prices[symbol]
		if !ok {
			return fmt.Errorf("could not get price for symbol %s", symbol)
		}
		currentPrice, _ := strconv.ParseFloat(currentPriceStr, 64)

		effectivePrice := currentPrice * (1 - feeRate)
		if effectivePrice > pair.Ratio {
			profit := (effectivePrice - pair.Ratio) / pair.Ratio
			e.logger.Info("Found profitable SELL opportunity",
				zap.String("symbol", symbol),
				zap.Float64("profit_margin", profit))
			e.executeTrade(&pair, binance.OrderSideSell)
		} else {
			e.logger.Info("No profitable SELL opportunity found this cycle.",
				zap.String("symbol", symbol),
				zap.Float64("current_price", currentPrice),
				zap.Float64("target_ratio", pair.Ratio))
		}
	}

	e.logger.Info("Scout cycle complete.")
	return nil
}

func (e *Engine) formatQuantity(symbol string, quantity float64) float64 {
	rule, ok := e.exchangeRules[symbol]
	if !ok {
		e.logger.Warn("No exchange rule found for symbol, using default formatting", zap.String("symbol", symbol))
		return quantity
	}

	var stepSize string
	for _, filter := range rule.Filters {
		if filter.FilterType == "LOT_SIZE" {
			stepSize = filter.StepSize
			break
		}
	}

	if stepSize == "" {
		e.logger.Warn("LOT_SIZE filter not found, using default formatting", zap.String("symbol", symbol))
		return quantity
	}

	// Find the number of decimal places from stepSize. "0.001" -> 3, "1.00" -> 0, "0.1000" -> 1
	precision := 0
	for i := len(stepSize) - 1; i >= 0; i-- {
		if stepSize[i] == '.' {
			break
		}
		if stepSize[i] == '1' {
			precision = i - 2 // (i-1) - 1
			if precision < 0 {
				precision = 0
			}
			break
		}
	}

	// Format the quantity to the required precision by flooring it.
	// e.g. quantity=1.23456, stepSize=0.001 -> precision=3 -> 1.234
	power := float64(10)
	for i := 0; i < precision; i++ {
		power *= 10
	}
	
	// Poor man's floor
	formattedQty, _ := strconv.ParseFloat(fmt.Sprintf("%.*f", precision, quantity), 64)
	return formattedQty
}

func (e *Engine) executeTrade(pair *models.Pair, side string) {
	symbol := pair.FromCoinSymbol + pair.ToCoinSymbol
	quantity := e.cfg.Trading.Quantity

	formattedQuantity := e.formatQuantity(symbol, quantity)

	l := e.logger.With(
		zap.String("symbol", symbol),
		zap.String("side", side),
		zap.Float64("original_quantity", quantity),
		zap.Float64("formatted_quantity", formattedQuantity),
	)

	if formattedQuantity <= 0 {
		l.Error("Formatted quantity is zero or less, skipping trade")
		return
	}

	l.Info("Executing trade...")

	var trade models.Trade
	var executedQty, quoteQty float64
	var err error

	if e.cfg.Trading.DryRun {
		l.Warn("Dry run enabled. No real trade will be executed.")
		// Simulate execution details for dry run
		executedQty = formattedQuantity
		quoteQty = formattedQuantity * pair.Ratio // Approximate quote quantity
	} else {
		var orderResponse *binance.CreateOrderResponse
		orderResponse, err = e.restClient.CreateOrder(symbol, side, formattedQuantity)
		if err != nil {
			l.Error("Failed to execute trade", zap.Error(err))
			return // Do not update DB if trade failed
		}
		l.Info("Trade executed successfully on Binance.")

		executedQty, _ = strconv.ParseFloat(orderResponse.ExecutedQuantity, 64)
		quoteQty, _ = strconv.ParseFloat(orderResponse.CummulativeQuoteQty, 64)
	}

	// Create and save the trade record
	price := 0.0
	if executedQty > 0 {
		price = quoteQty / executedQty
	}

	trade = models.Trade{
		Symbol:        symbol,
		Type:          side,
		Price:         price,
		Quantity:      executedQty,
		QuoteQuantity: quoteQty,
		Timestamp:     time.Now().Unix(),
		IsSimulation:  e.cfg.Trading.DryRun,
	}

	if err := e.db.Create(&trade).Error; err != nil {
		l.Error("Failed to save trade record to database", zap.Error(err))
		// We continue even if saving fails, to ensure the bot's state is updated.
	} else {
		l.Info("Successfully saved trade record", zap.Uint("trade_id", trade.ID))
	}


	// Update the current coin in the database
	var newCoinSymbol string
	if side == binance.OrderSideBuy {
		newCoinSymbol = pair.FromCoinSymbol
	} else { // SELL
		newCoinSymbol = pair.ToCoinSymbol
	}

	var currentCoin models.CurrentCoin
	e.db.First(&currentCoin)
	e.db.Model(&currentCoin).Update("symbol", newCoinSymbol)

	l.Info("Database updated. New current coin.", zap.String("new_coin", newCoinSymbol))
}