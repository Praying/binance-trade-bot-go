package models

import "gorm.io/gorm"

// Coin represents a tradable coin.
type Coin struct {
	gorm.Model
	Symbol   string  `gorm:"uniqueIndex"`
	Quantity float64 `gorm:"not null"`
	Enabled  bool    `gorm:"default:true"`
}
