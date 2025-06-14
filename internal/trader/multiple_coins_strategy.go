package trader

import (
	"binance-trade-bot-go/internal/models"
	"fmt"
	"go.uber.org/zap"
)

// MultipleCoinsStrategy scouts all configured coins to find the best trading opportunity.
type MultipleCoinsStrategy struct {
	// This strategy is stateless, so it doesn't need to hold any data.
}

// Name returns the unique name of the strategy.
func (s *MultipleCoinsStrategy) Name() string {
	return "MultipleCoins"
}

// Initialize ensures there are coins to trade in the database.
func (s *MultipleCoinsStrategy) Initialize(ctx StrategyContext) error {
	var count int64
	if err := ctx.DB.Model(&models.Pair{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count pairs in database: %w", err)
	}
	if count == 0 {
		ctx.Logger.Warn("No pairs found in the database. MultipleCoinsStrategy will not be able to trade.")
	}
	ctx.Logger.Info("MultipleCoinsStrategy initialized", zap.Int64("tradable_pairs", count))
	return nil
}

// Scout finds the best jump opportunity across all configured pairs.
func (s *MultipleCoinsStrategy) Scout(ctx StrategyContext) error {
	l := ctx.Logger.With(zap.String("strategy", s.Name()))
	l.Info("Scouting for trades across all pairs...")

	// 1. Get all ticker prices
	prices, err := ctx.RestClient.GetAllTickerPrices()
	if err != nil {
		return fmt.Errorf("could not get all ticker prices: %w", err)
	}

	// 2. Get all tradable pairs from the database
	var allPairs []models.Pair
	if err := ctx.DB.Find(&allPairs).Error; err != nil {
		return fmt.Errorf("could not fetch pairs: %w", err)
	}

	var bestOpp *tradeOpportunity

	// 3. Find the best opportunity among all pairs
	for _, pair := range allPairs {
		currentPair := pair
		profit, err := calculateProfitForPair(ctx, &currentPair, prices)
		if err != nil {
			l.Warn("Failed to calculate profit for pair", zap.String("pair", currentPair.FromCoinSymbol+"/"+currentPair.ToCoinSymbol), zap.Error(err))
			continue
		}

		if profit > 0 {
			if bestOpp == nil || profit > bestOpp.Profit {
				bestOpp = &tradeOpportunity{Pair: currentPair, Profit: profit}
			}
		}
	}

	// 4. Execute the jump if a profitable one was found
	if bestOpp != nil {
		l.Info("Found best overall jump opportunity",
			zap.String("from", bestOpp.Pair.FromCoinSymbol),
			zap.String("to", bestOpp.Pair.ToCoinSymbol),
			zap.Float64("profit_margin", bestOpp.Profit))

		// Find the quantity of the from coin
		var fromCoin models.Coin
		if err := ctx.DB.Where("symbol = ?", bestOpp.Pair.FromCoinSymbol).First(&fromCoin).Error; err != nil {
			return fmt.Errorf("could not find from_coin %s in db: %w", bestOpp.Pair.FromCoinSymbol, err)
		}

		// Execute the jump
		err := ExecuteJump(ctx, &bestOpp.Pair, fromCoin.Quantity, bestOpp.Profit)
		if err != nil {
			l.Error("Failed to execute best jump", zap.Error(err))
			return err
		}
		// The state update (e.g., coin quantities) should ideally be handled by a
		// separate portfolio manager that listens to trade events.
		// For now, we assume the core trading logic is complete after the jump.
		l.Info("Successfully executed jump.")
	} else {
		l.Info("No profitable jump opportunities found in this cycle.")
	}

	return nil
}
