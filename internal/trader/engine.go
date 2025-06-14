package trader

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
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

	// 1. Populate all-to-all pairs based on the config trade_pairs.
	e.logger.Info("Populating all-to-all trading pairs")
	for i := 0; i < len(tradeCoins); i++ {
		for j := 0; j < len(tradeCoins); j++ {
			if i == j {
				continue // Skip pairing a coin with itself
			}
			fromCoin := tradeCoins[i]
			toCoin := tradeCoins[j]

			pair := models.Pair{
				FromCoinSymbol: fromCoin,
				ToCoinSymbol:   toCoin,
			}
			// Create pair if it doesn't exist
			if err := e.db.FirstOrCreate(&pair, pair).Error; err != nil {
				return fmt.Errorf("failed to create pair %s/%s: %w", fromCoin, toCoin, err)
			}
		}
	}

	// 2. Set initial coin if not present.
	if err := e.ensureInitialCoin(tradeCoins, bridge); err != nil {
		return fmt.Errorf("failed to ensure initial coin: %w", err)
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
			// The ratio is FromCoin/ToCoin, which we calculate via the bridge currency.
			// Ratio = (FromCoin/Bridge) / (ToCoin/Bridge)
			fromSymbol := pair.FromCoinSymbol + bridge
			toSymbol := pair.ToCoinSymbol + bridge

			fromPriceStr, fromOk := prices[fromSymbol]
			toPriceStr, toOk := prices[toSymbol]

			if !fromOk {
				e.logger.Warn("Could not find from_coin price for ratio initialization", zap.String("symbol", fromSymbol))
				continue
			}
			if !toOk {
				e.logger.Warn("Could not find to_coin price for ratio initialization", zap.String("symbol", toSymbol))
				continue
			}

			fromPrice, err := strconv.ParseFloat(fromPriceStr, 64)
			if err != nil {
				e.logger.Error("Failed to parse from_price for ratio initialization",
					zap.String("symbol", fromSymbol), zap.String("price", fromPriceStr), zap.Error(err))
				continue
			}

			toPrice, err := strconv.ParseFloat(toPriceStr, 64)
			if err != nil {
				e.logger.Error("Failed to parse to_price for ratio initialization",
					zap.String("symbol", toSymbol), zap.String("price", toPriceStr), zap.Error(err))
				continue
			}

			if fromPrice > 0 && toPrice > 0 {
				ratio := fromPrice / toPrice
				pair.Ratio = ratio
				if err := e.db.Save(&pair).Error; err != nil {
					e.logger.Error("Failed to save initialized ratio", zap.String("pair", pair.FromCoinSymbol+"/"+pair.ToCoinSymbol), zap.Error(err))
				} else {
					e.logger.Info("Initialized ratio for pair", zap.String("pair", pair.FromCoinSymbol+"/"+pair.ToCoinSymbol), zap.Float64("ratio", ratio))
				}
			}
		}
	}
	return nil
}

// ensureInitialCoin makes sure there is a valid current coin to trade with.
func (e *Engine) ensureInitialCoin(tradeCoins []string, bridge string) error {
	var currentCoin models.CurrentCoin
	err := e.db.First(&currentCoin).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Case 1: No record exists, create a new one with a random trade coin.
		e.logger.Info("No current coin record found, selecting one randomly from trade pairs.")
		return e.setRandomInitialCoin(&currentCoin, tradeCoins, bridge)
	} else if err != nil {
		// Case 2: Any other database error.
		return err
	}

	// Case 3: Record exists, but is empty or set to the bridge currency. Reset it.
	if currentCoin.Symbol == "" || currentCoin.Symbol == bridge {
		e.logger.Warn("Current coin is empty or set to the bridge currency, re-initializing randomly.",
			zap.String("old_symbol", currentCoin.Symbol),
		)
		return e.setRandomInitialCoin(&currentCoin, tradeCoins, bridge)
	}

	e.logger.Info("Valid current coin found in database", zap.String("coin", currentCoin.Symbol))
	return nil
}

// setRandomInitialCoin selects a random coin from the trade list and sets it in the DB.
func (e *Engine) setRandomInitialCoin(currentCoin *models.CurrentCoin, tradeCoins []string, bridge string) error {
	rand.Seed(time.Now().UnixNano())

	var selectableCoins []string
	for _, coin := range tradeCoins {
		if coin != bridge {
			selectableCoins = append(selectableCoins, coin)
		}
	}

	if len(selectableCoins) == 0 {
		return fmt.Errorf("no selectable trade coins found in config, cannot set initial coin")
	}

	newSymbol := selectableCoins[rand.Intn(len(selectableCoins))]
	e.logger.Info("Setting current coin in DB", zap.String("coin", newSymbol))

	if currentCoin.ID == 0 { // Record does not exist yet
		currentCoin.Symbol = newSymbol
		return e.db.Create(currentCoin).Error
	}
	// Record exists, just update it
	return e.db.Model(currentCoin).Update("symbol", newSymbol).Error
}

// tradeOpportunity holds the details of a profitable trade.
type tradeOpportunity struct {
	Pair   models.Pair
	Profit float64
}

// scout performs a single round of scouting for profitable "jumps" from the current coin to another.
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

	// 3. Find all possible pairs to jump to from the current coin
	var pairs []models.Pair
	if err := e.db.Where("from_coin_symbol = ?", currentCoin.Symbol).Find(&pairs).Error; err != nil {
		return fmt.Errorf("could not get pairs for current coin %s: %w", currentCoin.Symbol, err)
	}

	// 4. Calculate profit for each possible jump concurrently
	var wg sync.WaitGroup
	opportunities := make(chan tradeOpportunity, len(pairs))

	for _, p := range pairs {
		wg.Add(1)
		go func(pair models.Pair) {
			defer wg.Done()
			profit, err := e.calculateProfitRatio(&pair, prices)
			if err != nil {
				e.logger.Warn("Failed to calculate profit ratio", zap.Error(err))
				return
			}

			if profit > 0 {
				e.logger.Debug("Potential jump opportunity",
					zap.String("from", pair.FromCoinSymbol),
					zap.String("to", pair.ToCoinSymbol),
					zap.Float64("profit_margin", profit),
				)
				opportunities <- tradeOpportunity{Pair: pair, Profit: profit}
			}
		}(p)
	}

	go func() {
		wg.Wait()
		close(opportunities)
	}()

	// 5. Find the best opportunity
	var bestOpp *tradeOpportunity
	for opp := range opportunities {
		if bestOpp == nil || opp.Profit > bestOpp.Profit {
			currentOpp := opp
			bestOpp = &currentOpp
		}
	}

	// 6. Execute the jump if a profitable one was found
	if bestOpp != nil {
		e.logger.Info("Found best jump opportunity",
			zap.String("from", bestOpp.Pair.FromCoinSymbol),
			zap.String("to", bestOpp.Pair.ToCoinSymbol),
			zap.Float64("profit_margin", bestOpp.Profit))
		e.executeJumpTransaction(&bestOpp.Pair)
	} else {
		e.logger.Info("No profitable jump opportunities found in this cycle.")
	}

	e.logger.Info("Scout cycle complete.")
	return nil
}

// calculateProfitRatio calculates the potential profit of jumping from FromCoin to ToCoin via the bridge.
func (e *Engine) calculateProfitRatio(pair *models.Pair, prices map[string]string) (float64, error) {
	bridge := e.cfg.Trading.Bridge

	// A "jump" to the bridge currency is not a valid trade, it's part of another jump.
	// This mirrors the Python version's logic where it would fail to get a price for "USDTUSDT" and skip.
	if pair.ToCoinSymbol == bridge {
		return 0, nil // Return zero profit and no error to safely skip this pair.
	}

	feeRate := e.cfg.Trading.FeeRate
	margin := e.cfg.Trading.ScoutMargin / 100 // Convert percentage to decimal

	// We need the prices of FromCoin/Bridge and ToCoin/Bridge
	fromSymbol := pair.FromCoinSymbol + bridge
	toSymbol := pair.ToCoinSymbol + bridge

	fromPriceStr, fromOk := prices[fromSymbol]
	toPriceStr, toOk := prices[toSymbol]

	if !fromOk {
		// Log this instead of returning a generic error, to make debugging clearer.
		// No need to bubble this up as a scout-stopping error.
		e.logger.Debug("Could not find price for source symbol", zap.String("symbol", fromSymbol))
		return 0, nil
	}
	if !toOk {
		// This can happen if the target coin is not paired with the bridge, which is a valid case to skip.
		e.logger.Debug("Could not find price for target symbol", zap.String("symbol", toSymbol))
		return 0, nil
	}

	fromPrice, _ := strconv.ParseFloat(fromPriceStr, 64)
	toPrice, _ := strconv.ParseFloat(toPriceStr, 64)

	if fromPrice == 0 || toPrice == 0 {
		return 0, fmt.Errorf("invalid prices for pair %s/%s", fromSymbol, toSymbol)
	}

	// This is the current market ratio for FromCoin/ToCoin calculated via the bridge
	currentRatio := fromPrice / toPrice

	// Simulate the fees for a two-step trade (sell FromCoin, buy ToCoin)
	// Simplified fee calculation: apply fee on both trades
	effectiveRatio := currentRatio * (1 - feeRate) * (1 - feeRate)

	// Profit is the percentage gain over the stored ratio, minus our margin
	profit := (effectiveRatio / pair.Ratio) - 1 - margin

	return profit, nil
}

// formatQuantity formats a quantity according to the symbol's LOT_SIZE filter rules.
// It returns the floored quantity and an error if the quantity is below the minimum.
func (e *Engine) formatQuantity(symbol string, quantity float64) (float64, error) {
	rule, ok := e.exchangeRules[symbol]
	if !ok {
		e.logger.Warn("No exchange rule found for symbol, using default formatting", zap.String("symbol", symbol))
		return quantity, nil
	}

	var stepSize, minQtyStr string
	for _, filter := range rule.Filters {
		if filter.FilterType == "LOT_SIZE" {
			stepSize = filter.StepSize
			minQtyStr = filter.MinQty
			break
		}
	}

	if stepSize == "" {
		e.logger.Warn("LOT_SIZE filter not found, using default formatting", zap.String("symbol", symbol))
		return quantity, nil
	}

	minQty, _ := strconv.ParseFloat(minQtyStr, 64)
	if quantity < minQty {
		return 0, fmt.Errorf("quantity %.8f is less than minQty %.8f for symbol %s", quantity, minQty, symbol)
	}

	// Robustly calculate precision from stepSize, e.g., "0.001000" -> 3
	var precision int
	dotIndex := -1
	for i, r := range stepSize {
		if r == '.' {
			dotIndex = i
			break
		}
	}

	if dotIndex != -1 {
		trimmed := ""
		for i := len(stepSize) - 1; i > dotIndex; i-- {
			if stepSize[i] != '0' {
				trimmed = stepSize[0 : i+1]
				break
			}
		}
		if trimmed != "" {
			precision = len(trimmed) - dotIndex - 1
		}
	}

	// Floor the quantity to the specified precision.
	// e.g. quantity=1.23456, precision=3 -> 1.234
	multiplier := math.Pow(10, float64(precision))
	floored := math.Floor(quantity*multiplier) / multiplier

	if floored < minQty {
		return 0, fmt.Errorf("formatted quantity %.8f is less than minQty %.8f for symbol %s", floored, minQty, symbol)
	}

	return floored, nil
}

// executeJumpTransaction performs a two-step trade: FromCoin -> Bridge -> ToCoin
func (e *Engine) executeJumpTransaction(pair *models.Pair) {
	bridge := e.cfg.Trading.Bridge
	fromCoin := pair.FromCoinSymbol
	toCoin := pair.ToCoinSymbol
	quantity := e.cfg.Trading.Quantity // This is the quantity of the FromCoin to sell

	l := e.logger.With(
		zap.String("from_coin", fromCoin),
		zap.String("to_coin", toCoin),
		zap.Float64("quantity", quantity),
	)
	l.Info("Executing jump transaction...")

	// --- Step 1: Sell FromCoin for Bridge Coin ---
	sellSymbol := fromCoin + bridge
	formattedSellQty, err := e.formatQuantity(sellSymbol, quantity)
	if err != nil {
		l.Error("Failed to format sell quantity, aborting jump.", zap.Error(err))
		return
	}

	var bridgeQtyObtained float64
	if e.cfg.Trading.DryRun {
		l.Warn("[Dry Run] Simulating SELL order", zap.String("symbol", sellSymbol))
		// Simulate getting price and calculating bridge amount
		prices, _ := e.restClient.GetAllTickerPrices()
		price, _ := strconv.ParseFloat(prices[sellSymbol], 64)
		bridgeQtyObtained = formattedSellQty * price * (1 - e.cfg.Trading.FeeRate)
	} else {
		sellOrder, err := e.restClient.CreateOrder(sellSymbol, binance.OrderSideSell, formattedSellQty)
		if err != nil {
			l.Error("Failed to execute SELL part of the jump", zap.Error(err))
			return
		}
		l.Info("SELL part of jump successful", zap.Int64("orderId", sellOrder.OrderID))
		bridgeQtyObtained, _ = strconv.ParseFloat(sellOrder.CummulativeQuoteQty, 64)
	}
	l.Info("Obtained bridge currency", zap.Float64("amount", bridgeQtyObtained))

	// --- Step 2: Buy ToCoin with Bridge Coin ---
	buySymbol := toCoin + bridge
	// We need to calculate the quantity of ToCoin to buy based on the bridge currency we have
	prices, _ := e.restClient.GetAllTickerPrices()
	toPrice, _ := strconv.ParseFloat(prices[buySymbol], 64)
	if toPrice == 0 {
		l.Error("Could not get price for ToCoin, aborting jump", zap.String("symbol", buySymbol))
		return
	}
	buyQuantity := bridgeQtyObtained / toPrice
	formattedBuyQty, err := e.formatQuantity(buySymbol, buyQuantity)
	if err != nil {
		l.Error("Failed to format buy quantity, aborting jump.", zap.Error(err))
		// Here you might want to handle the leftover bridge currency, e.g., by selling it back or flagging it.
		// For now, we abort.
		return
	}

	if e.cfg.Trading.DryRun {
		l.Warn("[Dry Run] Simulating BUY order", zap.String("symbol", buySymbol), zap.Float64("quantity", formattedBuyQty))
	} else {
		buyOrder, err := e.restClient.CreateOrder(buySymbol, binance.OrderSideBuy, formattedBuyQty)
		if err != nil {
			l.Error("Failed to execute BUY part of the jump", zap.Error(err))
			// At this point, we are holding bridge currency. The bot should ideally recover from this state.
			return
		}
		l.Info("BUY part of jump successful", zap.Int64("orderId", buyOrder.OrderID))
	}

	// --- Step 3: Update State ---
	// Update current coin in DB
	var currentCoin models.CurrentCoin
	e.db.First(&currentCoin)
	e.db.Model(&currentCoin).Update("symbol", toCoin)
	l.Info("Database updated. New current coin.", zap.String("new_coin", toCoin))

	// Update ratios for the new coin
	e.updateRatiosForNewCoin(toCoin, prices)
}

// updateRatiosForNewCoin updates all pairs where the FromCoin is the newly acquired coin.
func (e *Engine) updateRatiosForNewCoin(newCoin string, prices map[string]string) {
	bridge := e.cfg.Trading.Bridge
	var pairsToUpdate []models.Pair
	e.db.Where("from_coin_symbol = ?", newCoin).Find(&pairsToUpdate)

	l := e.logger.With(zap.String("new_coin", newCoin))
	l.Info("Updating ratios for new coin...")

	for _, pair := range pairsToUpdate {
		// Ratio is FromCoin/ToCoin, calculated via bridge
		fromSymbol := pair.FromCoinSymbol + bridge
		toSymbol := pair.ToCoinSymbol + bridge

		fromPriceStr, fromOk := prices[fromSymbol]
		toPriceStr, toOk := prices[toSymbol]

		if !fromOk || !toOk {
			l.Warn("Could not find prices to update ratio", zap.String("pair", fromSymbol+"/"+toSymbol))
			continue
		}
		fromPrice, _ := strconv.ParseFloat(fromPriceStr, 64)
		toPrice, _ := strconv.ParseFloat(toPriceStr, 64)

		if fromPrice > 0 && toPrice > 0 {
			newRatio := fromPrice / toPrice
			e.db.Model(&pair).Update("ratio", newRatio)
			l.Info("Updated ratio", zap.String("pair", pair.FromCoinSymbol+"/"+pair.ToCoinSymbol), zap.Float64("new_ratio", newRatio))
		}
	}
}
