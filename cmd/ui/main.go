package main

import (
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/database"
	"binance-trade-bot-go/internal/logger"
	"binance-trade-bot-go/internal/models"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// APIHandler holds dependencies for the API endpoints.
type APIHandler struct {
	log        *zap.Logger
	db         *gorm.DB
	traderURLs []string
}

// NewAPIHandler creates a new APIHandler.
func NewAPIHandler(log *zap.Logger, db *gorm.DB, traderURLs []string) *APIHandler {
	return &APIHandler{log: log, db: db, traderURLs: traderURLs}
}

// TraderStatus represents the status of a single trader instance.
type TraderStatus struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	Strategy  string `json:"strategy"`
	StartTime string `json:"start_time"`
	Uptime    string `json:"uptime"`
	IsHealthy bool   `json:"is_healthy"`
	Error     string `json:"error,omitempty"`
}

// TradersHandler fetches the status of all configured traders.
func (h *APIHandler) TradersHandler(w http.ResponseWriter, r *http.Request) {
	var statuses []TraderStatus
	client := &http.Client{Timeout: 5 * time.Second}

	for _, url := range h.traderURLs {
		statusURL := url + "/status"
		healthURL := url + "/health"

		var status TraderStatus

		// Check health first
		resp, err := client.Get(healthURL)
		if err != nil || resp.StatusCode != http.StatusOK {
			status.IsHealthy = false
			if err != nil {
				status.Error = err.Error()
			} else {
				status.Error = "unhealthy status code"
			}
			statuses = append(statuses, status)
			continue
		}

		status.IsHealthy = true

		// Fetch status
		resp, err = client.Get(statusURL)
		if err != nil {
			status.IsHealthy = false
			status.Error = err.Error()
			statuses = append(statuses, status)
			continue
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			status.IsHealthy = false
			status.Error = "failed to decode status"
			statuses = append(statuses, status)
			continue
		}

		statuses = append(statuses, status)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(statuses); err != nil {
		h.log.Error("Failed to encode trader statuses", zap.Error(err))
		http.Error(w, "Failed to encode statuses", http.StatusInternalServerError)
	}
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

func main() {
	// Load configuration
	cfg, err := config.LoadConfig("./configs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.NewLogger(cfg.Logger.Level, cfg.Logger.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer log.Sync()

	// Connect to the database
	db, err := database.NewDatabase(&cfg)
	if err != nil {
		log.Fatal("Failed to connect to database", zap.Error(err))
	}

	// Auto-migrate the schema
	log.Info("Running database migrations...")
	if err := db.AutoMigrate(&models.Coin{}, &models.Pair{}, &models.Trade{}); err != nil {
		log.Fatal("Failed to migrate database", zap.Error(err))
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Create a handler that has access to the logger and db
	apiHandler := NewAPIHandler(log, db, cfg.Server.TraderURLs)

	// API endpoints
	mux.HandleFunc("/api/trades", apiHandler.TradesHandler)
	mux.HandleFunc("/api/statistics", apiHandler.StatisticsHandler)
	mux.HandleFunc("/api/traders", apiHandler.TradersHandler)

	// Static file serving for CSS, JS, etc.
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// HTML template serving
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "web/templates/index.html")
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	log.Info("Starting web server", zap.String("address", addr))

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal("Web server failed", zap.Error(err))
	}
}
