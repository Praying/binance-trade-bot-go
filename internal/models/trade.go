package models

import "gorm.io/gorm"

// Trade represents a completed trade record in the database.
type Trade struct {
	gorm.Model
	Symbol        string  `json:"symbol"`
	Type          string  `json:"type"` // "BUY" or "SELL"
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
	QuoteQuantity float64 `json:"quote_quantity"`
	Timestamp     int64   `json:"timestamp"`
	IsSimulation  bool    `json:"is_simulation"`
	Profit        float64 `json:"profit,omitempty"`
}
