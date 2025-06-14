package trader

import (
	"binance-trade-bot-go/internal/models"
	"fmt"
	"go.uber.org/zap"
	"sync"
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
	if err := ctx.DB.Model(&models.Coin{}).Count(&count).Error; err != nil {
		return fmt.Errorf("failed to count coins in database: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("no coins found in database to initialize strategy")
	}
	ctx.Logger.Info("MultipleCoinsStrategy initialized", zap.Int64("tradable_coins", count))
	return nil
}

// Scout finds the best jump opportunity across all configured coins.
func (s *MultipleCoinsStrategy) Scout(ctx StrategyContext) error {
	l := ctx.Logger.With(zap.String("strategy", s.Name()))
	l.Info("Scouting for trades across all coins...")

	// 1. Get all ticker prices
	prices, err := ctx.RestClient.GetAllTickerPrices()
	if err != nil {
		return fmt.Errorf("could not get all ticker prices: %w", err)
	}

	// 2. Get all tradable coins from the database
	var coins []models.Coin
	if err := ctx.DB.Find(&coins).Error; err != nil {
		return fmt.Errorf("could not fetch coins for scouting: %w", err)
	}

	// 3. Find the best opportunity across all coins concurrently
	var wg sync.WaitGroup
	opportunities := make(chan *tradeOpportunity, len(coins))

	for _, c := range coins {
		wg.Add(1)
		go func(coin models.Coin) {
			defer wg.Done()
			opp, err := findBestJump(ctx, &coin, prices)
			if err != nil {
				l.Warn("Failed to find best jump for coin", zap.String("coin", coin.Symbol), zap.Error(err))
				return
			}
			if opp != nil {
				opportunities <- opp
			}
		}(c)
	}

	go func() {
		wg.Wait()
		close(opportunities)
	}()

	// 4. Find the absolute best opportunity from all concurrent scouts
	var bestOpp *tradeOpportunity
	for opp := range opportunities {
		if bestOpp == nil || opp.Profit > bestOpp.Profit {
			bestOpp = opp
		}
	}

	// 5. Execute the jump if a profitable one was found
	if bestOpp != nil {
		l.Info("Found best jump opportunity globally",
			zap.String("from", bestOpp.Pair.FromCoinSymbol),
			zap.String("to", bestOpp.Pair.ToCoinSymbol),
			zap.Float64("profit_margin", bestOpp.Profit),
		)

		err := ExecuteJump(ctx, &bestOpp.Pair, ctx.Cfg.Trading.Quantity)
		if err != nil {
			l.Error("Failed to execute jump", zap.Error(err))
			return err
		}
		// Unlike DefaultStrategy, we don't need to update any state here because
		// the next scout cycle will again check all coins.
	} else {
		l.Info("No profitable jump opportunities found in this cycle.")
	}

	return nil
}
