package models

import "gorm.io/gorm"

// Coin represents a tradable coin.
type Coin struct {
	gorm.Model
	Symbol  string `gorm:"uniqueIndex"`
	Enabled bool   `gorm:"default:true"`
}
