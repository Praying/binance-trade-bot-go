package database

import (
	"binance-trade-bot-go/internal/models"
	"fmt"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NewDatabase creates a new database connection and performs auto-migration.
func NewDatabase(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Auto-migrate the schema
	err = AutoMigrate(db)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// AutoMigrate runs GORM's auto-migration feature.
func AutoMigrate(db *gorm.DB) error {
	err := db.AutoMigrate(
		&models.Trade{},
		&models.CurrentCoin{},
		&models.Pair{},
	)
	if err != nil {
		return fmt.Errorf("failed to auto-migrate database: %w", err)
	}
	return nil
}
