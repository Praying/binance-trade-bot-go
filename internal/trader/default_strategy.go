package trader

import (
	"binance-trade-bot-go/internal/models"
	"fmt"
	"go.uber.org/zap"
	"math/rand"
	"time"
)

type DefaultStrategy struct {
	lastUsedCoinSymbol string
}

func (s *DefaultStrategy) Name() string {
	return "Default"
}

func (s *DefaultStrategy) Initialize(ctx StrategyContext) error {
	var coins []models.Coin
	if err := ctx.DB.Find(&coins).Error; err != nil {
		return fmt.Errorf("could not fetch coins for initialization: %w", err)
	}
	if len(coins) == 0 {
		ctx.Logger.Warn("No coins found in the database. DefaultStrategy will not be able to trade.")
		return nil
	}

	// In Python version, it could be specified in config, here we just pick one
	rand.Seed(time.Now().UnixNano())
	s.lastUsedCoinSymbol = coins[rand.Intn(len(coins))].Symbol
	ctx.Logger.Info("DefaultStrategy initialized", zap.String("initial_coin", s.lastUsedCoinSymbol))

	// In Python version, it would buy the initial coin if not present.
	// Here, we assume the user has funds and the logic will be handled in scout.
	return nil
}

func (s *DefaultStrategy) Scout(ctx StrategyContext) error {
	l := ctx.Logger.With(zap.String("strategy", s.Name()))

	// 1. Get all ticker prices
	prices, err := ctx.RestClient.GetAllTickerPrices()
	if err != nil {
		return fmt.Errorf("could not get all ticker prices: %w", err)
	}

	// 2. Find the current coin to scout with
	var currentCoin models.Coin
	if err := ctx.DB.Where("symbol = ?", s.lastUsedCoinSymbol).First(&currentCoin).Error; err != nil {
		return fmt.Errorf("could not find last used coin %s in db: %w", s.lastUsedCoinSymbol, err)
	}
	l.Info("Scouting for trades...", zap.String("from_coin", currentCoin.Symbol))

	// 3. Find the best jump opportunity
	bestOpp, err := findBestJump(ctx, &currentCoin, prices)
	if err != nil {
		return err
	}

	// 4. Execute the jump if a profitable one was found
	if bestOpp != nil {
		l.Info("Found best jump opportunity",
			zap.String("from", bestOpp.Pair.FromCoinSymbol),
			zap.String("to", bestOpp.Pair.ToCoinSymbol),
			zap.Float64("profit_margin", bestOpp.Profit))

		// Execute the jump using the helper function
		err := ExecuteJump(ctx, &bestOpp.Pair, ctx.Cfg.Trading.Quantity)
		if err != nil {
			l.Error("Failed to execute jump", zap.Error(err))
			// If the jump fails, we don't update the coin, we'll retry on the next tick.
			return err
		}

		// Update the last used coin for the next scout cycle
		s.lastUsedCoinSymbol = bestOpp.Pair.ToCoinSymbol
	} else {
		l.Info("No profitable jump opportunities found in this cycle.")
	}

	return nil
}
