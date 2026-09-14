package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"KubeDiskGuard/pkg/service"

	"github.com/gorilla/mux"
)

// APIServer exposes the static policy executor's operational state.
type APIServer struct {
	service *service.KubeDiskGuardService
}

func NewAPIServer(svc *service.KubeDiskGuardService) *APIServer {
	return &APIServer{service: svc}
}

func (s *APIServer) RegisterRoutes(router *mux.Router) {
	apiRouter := router.PathPrefix("/api/v1").Subrouter()
	apiRouter.Use(s.corsMiddleware)
	apiRouter.Use(s.loggingMiddleware)
	apiRouter.HandleFunc("/limits", s.handleGetAllLimits).Methods(http.MethodGet)
	apiRouter.HandleFunc("/limits/{id}", s.handleGetLimit).Methods(http.MethodGet)
	apiRouter.HandleFunc("/health", s.handleHealth).Methods(http.MethodGet)
	apiRouter.HandleFunc("/info", s.handleInfo).Methods(http.MethodGet)
}

func (s *APIServer) HandleReadiness(w http.ResponseWriter, _ *http.Request) {
	status := s.service.GetServiceStatus()
	if status.LastReconcileAt.IsZero() || status.LastError != "" {
		s.writeJSONResponse(w, map[string]interface{}{"status": "not_ready", "error": status.LastError}, http.StatusServiceUnavailable)
		return
	}
	s.writeJSONResponse(w, map[string]interface{}{"status": "ready"}, http.StatusOK)
}

func (s *APIServer) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *APIServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[API] %s %s - %v", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *APIServer) writeJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[API] Error encoding JSON response: %v", err)
	}
}

func (s *APIServer) writeErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	s.writeJSONResponse(w, map[string]interface{}{"error": true, "message": message, "code": statusCode}, statusCode)
}
