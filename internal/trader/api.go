package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// APIServer provides an HTTP interface for the trading engine.
type APIServer struct {
	server *http.Server
	engine *Engine
	logger *zap.Logger
}

// NewAPIServer creates a new APIServer.
func NewAPIServer(engine *Engine, logger *zap.Logger) *APIServer {
	addr := fmt.Sprintf(":%d", engine.cfg.Trading.ApiPort)
	server := &http.Server{
		Addr: addr,
	}

	return &APIServer{
		server: server,
		engine: engine,
		logger: logger.Named("api-server"),
	}
}

// Start runs the HTTP server in a new goroutine.
func (s *APIServer) Start() {
	http.HandleFunc("/status", s.statusHandler)
	http.HandleFunc("/health", s.healthHandler)
	// We will add /stop and /restart handlers later

	s.logger.Info("Starting API server", zap.String("address", s.server.Addr))
	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			s.logger.Error("API server failed", zap.Error(err))
		}
	}()
}

// Stop gracefully shuts down the server.
func (s *APIServer) Stop(ctx context.Context) error {
	s.logger.Info("Stopping API server...")
	return s.server.Shutdown(ctx)
}

func (s *APIServer) statusHandler(w http.ResponseWriter, r *http.Request) {
	status := struct {
		UUID      string `json:"uuid"`
		Name      string `json:"name"`
		Strategy  string `json:"strategy"`
		StartTime string `json:"start_time"`
		Uptime    string `json:"uptime"`
	}{
		UUID:      s.engine.UUID,
		Name:      s.engine.Name,
		Strategy:  s.engine.strategy.Name(),
		StartTime: s.engine.StartTime.Format(time.RFC3339),
		Uptime:    time.Since(s.engine.StartTime).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("Failed to write status response", zap.Error(err))
		http.Error(w, "Failed to encode status", http.StatusInternalServerError)
	}
}

func (s *APIServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
