package main

import (
	"binance-trade-bot-go/internal/models"
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// APIHandler holds dependencies for the API endpoints.
type APIHandler struct {
	log *zap.Logger
	db  *gorm.DB
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(log *zap.Logger, db *gorm.DB) *APIHandler {
	return &APIHandler{log: log, db: db}
}

// StatusHandler returns the current status of the bot (i.e., the currently held coin).
func (h *APIHandler) StatusHandler(w http.ResponseWriter, r *http.Request) {
	var currentCoin models.CurrentCoin
	if err := h.db.First(&currentCoin).Error; err != nil {
		h.log.Error("Failed to get current coin status", zap.Error(err))
		http.Error(w, "Failed to get status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentCoin)
}

// TradesHandler returns all historical trades.
func (h *APIHandler) TradesHandler(w http.ResponseWriter, r *http.Request) {
	var trades []models.Trade
	// Order by most recent first
	if err := h.db.Order("timestamp desc").Find(&trades).Error; err != nil {
		h.log.Error("Failed to get trades from database", zap.Error(err))
		http.Error(w, "Failed to get trades", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trades)
}
