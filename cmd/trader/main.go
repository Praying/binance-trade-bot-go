package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"binance-trade-bot-go/internal/binance"
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/database"
	"binance-trade-bot-go/internal/logger"
	"binance-trade-bot-go/internal/trader"
	"go.uber.org/zap"
)

func main() {
	// Load application configuration
	cfg, err := config.LoadConfig("./configs")
	if err != nil {
		// We can't use the logger here because it's not initialized yet.
		panic(fmt.Sprintf("could not load config: %v", err))
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg.Logger.Level, cfg.Logger.Format)
	if err != nil {
		panic(err)
	}
	defer log.Sync()
	log.Info("Configuration loaded")

	// Initialize database
	db, err := database.NewDatabase(&cfg)
	if err != nil {
		log.Fatal("Failed to connect to database", zap.Error(err))
	}
	log.Info("Database connection successful and schema migrated.")

	// Initialize Binance REST client
	restClient := binance.NewRestClient(&cfg.Binance, log)
	if _, err := restClient.GetServerTime(); err != nil {
		log.Fatal("Failed to connect to Binance API", zap.Error(err))
	}
	log.Info("Successfully connected to Binance API.")

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		sigchan := make(chan os.Signal, 1)
		signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM)
		<-sigchan
		log.Info("Shutdown signal received, gracefully shutting down...")
		cancel()
	}()

	// --- Strategy and Engine Setup ---
	var selectedStrategy trader.Strategy
	strategyName := cfg.Trading.Strategy

	switch strategyName {
	case "Default":
		selectedStrategy = &trader.DefaultStrategy{}
	case "MultipleCoins":
		selectedStrategy = &trader.MultipleCoinsStrategy{}
	default:
		log.Fatal("Invalid strategy specified in config", zap.String("strategy", strategyName))
	}

	log.Info("Using strategy", zap.String("strategy", selectedStrategy.Name()))

	// Initialize and run the trading engine with the selected strategy
	tradeEngine := trader.NewEngine(log, &cfg, restClient, db, selectedStrategy)
	tradeEngine.Run(ctx)

	log.Info("Bot has been shut down.")
}
