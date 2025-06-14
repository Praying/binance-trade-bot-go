package models

import "gorm.io/gorm"

// CurrentCoin represents the coin currently held by the bot.
// There should only ever be one row in this table.
type CurrentCoin struct {
	gorm.Model
	Symbol string `gorm:"unique;not null"`
}
