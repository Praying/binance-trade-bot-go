package database

import (
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/models"
	"fmt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NewDatabase creates a new database connection and performs auto-migration.
func NewDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(cfg.Database.DSN), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Auto-migrate the schema
	err = AutoMigrate(db, cfg)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// AutoMigrate drops existing tables, creates new ones, and populates initial data.
func AutoMigrate(db *gorm.DB, cfg *config.Config) error {
	// Drop all existing tables to ensure a clean state
	if err := db.Migrator().DropTable(&models.Trade{}, &models.CurrentCoin{}, &models.Pair{}, &models.Coin{}); err != nil {
		// We can ignore "not found" errors, but fail on others
		if err.Error() != "table not found" {
			return fmt.Errorf("failed to drop tables: %w", err)
		}
	}

	// Create new tables based on the current models
	if err := db.AutoMigrate(&models.Trade{}, &models.Pair{}, &models.Coin{}); err != nil {
		return fmt.Errorf("failed to auto-migrate database: %w", err)
	}

	// Populate the 'coins' table from the config
	allCoins := make(map[string]struct{})
	for _, pair := range cfg.Trading.TradePairs {
		allCoins[pair] = struct{}{}
	}

	for coinSymbol := range allCoins {
		coin := models.Coin{Symbol: coinSymbol, Enabled: true}
		if err := db.FirstOrCreate(&coin, models.Coin{Symbol: coinSymbol}).Error; err != nil {
			return fmt.Errorf("failed to populate coin '%s': %w", coinSymbol, err)
		}
	}

	return nil
}
