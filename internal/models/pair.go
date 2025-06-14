package models

import "gorm.io/gorm"

// Pair represents a trading pair between two coins.
// It also stores the initial ratio used as a benchmark for trading.
type Pair struct {
	gorm.Model
	FromCoinSymbol string  `gorm:"uniqueIndex:idx_from_to"`
	ToCoinSymbol   string  `gorm:"uniqueIndex:idx_from_to"`
	Ratio          float64 `gorm:"not null"`
}
