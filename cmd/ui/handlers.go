package main

import (
	"binance-trade-bot-go/internal/models"
	"encoding/json"
	"net/http"
	"time"

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

// StatsDetail holds calculated statistics for a given period.
type StatsDetail struct {
	TotalTrades      int64   `json:"total_trades"`
	ProfitableTrades int64   `json:"profitable_trades"`
	WinRate          float64 `json:"win_rate"`
	TotalProfit      float64 `json:"total_profit"`
}

// StatisticsResponse is the structure for the /api/statistics endpoint.
type StatisticsResponse struct {
	Since24h StatsDetail `json:"since_24h"`
	AllTime  StatsDetail `json:"all_time"`
}

// StatisticsHandler calculates and returns trading statistics.
func (h *APIHandler) StatisticsHandler(w http.ResponseWriter, r *http.Request) {
	var allTrades []models.Trade
	if err := h.db.Where("profit != ?", 0).Find(&allTrades).Error; err != nil {
		h.log.Error("Failed to get trades for statistics", zap.Error(err))
		http.Error(w, "Failed to calculate statistics", http.StatusInternalServerError)
		return
	}

	now := time.Now()
	since24h := now.Add(-24 * time.Hour)

	stats24h := StatsDetail{}
	statsAllTime := StatsDetail{}

	for _, trade := range allTrades {
		// Calculate for all time
		statsAllTime.TotalTrades++
		if trade.Profit > 0 {
			statsAllTime.ProfitableTrades++
		}
		statsAllTime.TotalProfit += trade.Profit

		// Calculate for last 24 hours
		tradeTime := time.Unix(trade.Timestamp/1000, 0)
		if tradeTime.After(since24h) {
			stats24h.TotalTrades++
			if trade.Profit > 0 {
				stats24h.ProfitableTrades++
			}
			stats24h.TotalProfit += trade.Profit
		}
	}

	if statsAllTime.TotalTrades > 0 {
		statsAllTime.WinRate = float64(statsAllTime.ProfitableTrades) / float64(statsAllTime.TotalTrades)
	}
	if stats24h.TotalTrades > 0 {
		stats24h.WinRate = float64(stats24h.ProfitableTrades) / float64(stats24h.TotalTrades)
	}

	response := StatisticsResponse{
		Since24h: stats24h,
		AllTime:  statsAllTime,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
