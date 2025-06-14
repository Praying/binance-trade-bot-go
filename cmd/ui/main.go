package main

import (
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/database"
	"binance-trade-bot-go/internal/logger"
	"binance-trade-bot-go/internal/models"
	"fmt"
	"net/http"
	"os"

	"go.uber.org/zap"
)

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
	apiHandler := NewAPIHandler(log, db)

	// API endpoints
	mux.HandleFunc("/api/trades", apiHandler.TradesHandler)
	mux.HandleFunc("/api/statistics", apiHandler.StatisticsHandler)

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
