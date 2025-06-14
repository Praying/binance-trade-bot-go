package trader

import (
	"binance-trade-bot-go/internal/models"
	"fmt"
	"go.uber.org/zap"
	"math"
	"strconv"
	"sync"
)

// tradeOpportunity holds the details of a profitable trade.
type tradeOpportunity struct {
	Pair   models.Pair
	Profit float64
}

// findBestJump searches for the most profitable trade from a given source coin.
func findBestJump(ctx StrategyContext, fromCoin *models.Coin, prices map[string]string) (*tradeOpportunity, error) {
	var pairs []models.Pair
	if err := ctx.DB.Where("from_coin_symbol = ?", fromCoin.Symbol).Find(&pairs).Error; err != nil {
		return nil, fmt.Errorf("could not get pairs for coin %s: %w", fromCoin.Symbol, err)
	}

	if len(pairs) == 0 {
		return nil, nil // No pairs configured for this coin
	}

	var wg sync.WaitGroup
	opportunities := make(chan tradeOpportunity, len(pairs))

	for _, p := range pairs {
		wg.Add(1)
		go func(pair models.Pair) {
			defer wg.Done()
			profit, err := calculateProfitForPair(ctx, &pair, prices)
			if err != nil {
				ctx.Logger.Warn("Failed to calculate profit for pair", zap.String("pair", pair.FromCoinSymbol+"/"+pair.ToCoinSymbol), zap.Error(err))
				return
			}

			if profit > 0 {
				opportunities <- tradeOpportunity{Pair: pair, Profit: profit}
			}
		}(p)
	}

	go func() {
		wg.Wait()
		close(opportunities)
	}()

	var bestOpp *tradeOpportunity
	for opp := range opportunities {
		if bestOpp == nil || opp.Profit > bestOpp.Profit {
			currentOpp := opp
			bestOpp = &currentOpp
		}
	}

	return bestOpp, nil
}

// calculateProfitForPair is the core profit calculation logic.
func calculateProfitForPair(ctx StrategyContext, pair *models.Pair, prices map[string]string) (float64, error) {
	bridge := "USDT" // Default to USDT for now
	if ctx.Cfg.Trading.Bridge != "" {
		bridge = ctx.Cfg.Trading.Bridge
	}

	if pair.ToCoinSymbol == bridge {
		return 0, nil
	}

	feeRate := ctx.Cfg.Trading.FeeRate
	margin := ctx.Cfg.Trading.ScoutMargin / 100

	fromSymbol := pair.FromCoinSymbol + bridge
	toSymbol := pair.ToCoinSymbol + bridge

	fromPriceStr, fromOk := prices[fromSymbol]
	toPriceStr, toOk := prices[toSymbol]

	if !fromOk || !toOk {
		return 0, fmt.Errorf("prices not available for pair %s/%s", fromSymbol, toSymbol)
	}

	fromPrice, err1 := strconv.ParseFloat(fromPriceStr, 64)
	toPrice, err2 := strconv.ParseFloat(toPriceStr, 64)

	if err1 != nil || err2 != nil {
		return 0, fmt.Errorf("failed to parse prices for pair %s/%s", fromSymbol, toSymbol)
	}

	if fromPrice == 0 || toPrice == 0 {
		return 0, fmt.Errorf("invalid prices for pair %s/%s", fromSymbol, toSymbol)
	}

	currentRatio := fromPrice / toPrice
	effectiveRatio := currentRatio * (1 - feeRate) * (1 - feeRate)
	profit := (effectiveRatio / pair.Ratio) - 1 - margin

	return profit, nil
}

// formatQuantity formats a quantity according to the symbol's LOT_SIZE filter rules.
func formatQuantity(ctx StrategyContext, symbol string, quantity float64) (float64, error) {
	rule, ok := ctx.ExchangeRules[symbol]
	if !ok {
		ctx.Logger.Warn("No exchange rule found for symbol, using default formatting", zap.String("symbol", symbol))
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
		ctx.Logger.Warn("LOT_SIZE filter not found, using default formatting", zap.String("symbol", symbol))
		return quantity, nil
	}

	minQty, _ := strconv.ParseFloat(minQtyStr, 64)
	if quantity < minQty {
		return 0, fmt.Errorf("quantity %.8f is less than minQty %.8f for symbol %s", quantity, minQty, symbol)
	}

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

	multiplier := math.Pow(10, float64(precision))
	floored := math.Floor(quantity*multiplier) / multiplier

	if floored < minQty {
		return 0, fmt.Errorf("formatted quantity %.8f is less than minQty %.8f for symbol %s", floored, minQty, symbol)
	}

	return floored, nil
}

// ExecuteJump performs a two-step trade and records it in the database.
func ExecuteJump(ctx StrategyContext, pair *models.Pair, fromCoinQuantity float64, profit float64) error {
	bridge := ctx.Cfg.Trading.Bridge
	fromCoin := pair.FromCoinSymbol
	toCoin := pair.ToCoinSymbol

	l := ctx.Logger.With(
		zap.String("from_coin", fromCoin),
		zap.String("to_coin", toCoin),
		zap.Float64("quantity", fromCoinQuantity),
	)
	l.Info("Executing jump transaction...")

	// --- Step 1: Sell FromCoin for Bridge Coin ---
	sellSymbol := fromCoin + bridge
	formattedSellQty, err := formatQuantity(ctx, sellSymbol, fromCoinQuantity)
	if err != nil {
		l.Error("Failed to format sell quantity, aborting jump.", zap.Error(err))
		return err
	}

	sellOrder, err := ctx.RestClient.CreateOrder(sellSymbol, "SELL", formattedSellQty)
	if err != nil {
		return fmt.Errorf("failed to execute sell order for %s: %w", sellSymbol, err)
	}
	// In a real scenario, we'd wait for the order to fill. For now, we simulate it.
	prices, _ := ctx.RestClient.GetAllTickerPrices()
	price, _ := strconv.ParseFloat(prices[sellSymbol], 64)
	bridgeQtyObtained := formattedSellQty * price * (1 - ctx.Cfg.Trading.FeeRate)
	l.Info("Sell order created", zap.Int64("orderId", sellOrder.OrderID))

	// Record the SELL trade
	sellTrade := models.Trade{
		Symbol:        sellSymbol,
		Type:          "SELL",
		Price:         price,
		Quantity:      formattedSellQty,
		QuoteQuantity: formattedSellQty * price,
		Timestamp:     sellOrder.TransactTime,
		// IsSimulation:  ctx.Cfg.Trading.IsSimulation,
	}
	if err := ctx.DB.Create(&sellTrade).Error; err != nil {
		l.Error("Failed to record sell trade", zap.Error(err))
		// Continue even if recording fails, as the trade itself succeeded.
	}

	// --- Step 2: Buy ToCoin with Bridge Coin ---
	buySymbol := toCoin + bridge
	prices, _ = ctx.RestClient.GetAllTickerPrices()
	toPrice, _ := strconv.ParseFloat(prices[buySymbol], 64)
	if toPrice == 0 {
		l.Error("Could not get price for ToCoin, aborting jump", zap.String("symbol", buySymbol))
		return fmt.Errorf("could not get price for %s", buySymbol)
	}
	buyQuantity := bridgeQtyObtained / toPrice
	formattedBuyQty, err := formatQuantity(ctx, buySymbol, buyQuantity)
	if err != nil {
		l.Error("Failed to format buy quantity, aborting jump.", zap.Error(err))
		return err
	}

	formattedBuyQty, err = formatQuantity(ctx, buySymbol, buyQuantity)
	if err != nil {
		l.Error("Failed to format buy quantity, aborting jump.", zap.Error(err))
		return err
	}
	buyOrder, err := ctx.RestClient.CreateOrder(buySymbol, "BUY", formattedBuyQty)
	if err != nil {
		return fmt.Errorf("failed to execute buy order for %s: %w", buySymbol, err)
	}
	l.Info("Buy order created", zap.Int64("orderId", buyOrder.OrderID))

	// Record the BUY trade with profit
	buyTrade := models.Trade{
		Symbol:        buySymbol,
		Type:          "BUY",
		Price:         toPrice,
		Quantity:      formattedBuyQty,
		QuoteQuantity: formattedBuyQty * toPrice,
		Timestamp:     buyOrder.TransactTime,
		// IsSimulation:  ctx.Cfg.Trading.IsSimulation,
		Profit: profit, // Store the overall profit in the final leg of the jump
	}
	if err := ctx.DB.Create(&buyTrade).Error; err != nil {
		l.Error("Failed to record buy trade", zap.Error(err))
		// Continue even if recording fails
	}

	l.Info("Jump transaction successful.", zap.String("new_coin", toCoin))

	return nil
}
