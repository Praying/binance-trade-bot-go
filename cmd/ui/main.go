package main

import (
	"binance-trade-bot-go/internal/config"
	"binance-trade-bot-go/internal/database"
	"binance-trade-bot-go/internal/logger"
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
	db, err := database.NewDatabase(cfg.Database.DSN)
	if err != nil {
		log.Fatal("Failed to connect to database", zap.Error(err))
	}

	// Setup HTTP server
	mux := http.NewServeMux()

	// Create a handler that has access to the logger and db
	apiHandler := NewAPIHandler(log, db)

	// API endpoints
	mux.HandleFunc("/api/status", apiHandler.StatusHandler)
	mux.HandleFunc("/api/trades", apiHandler.TradesHandler)

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
