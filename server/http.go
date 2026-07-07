package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// ListenAndServe builds the Chi router and starts the HTTP server on the given port.
func (s *Server) ListenAndServe(port int) error {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Route("/gobbler", func(r chi.Router) {
		r.Get("/", s.handleRootDiscovery)
		r.Route("/definition", s.definitionRoutes)
		r.Route("/pipeline", s.pipelineRoutes)
		r.Route("/ingest", s.ingestRoutes)
		r.Route("/query", s.queryRoutes)
	})

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting Gobbler server on http://localhost%s", addr)
	return http.ListenAndServe(addr, r)
}

func (s *Server) handleRootDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Gobbler ingestion pipeline REST API",
		"route_groups": []string{
			"/gobbler/definition",
			"/gobbler/pipeline",
			"/gobbler/ingest",
			"/gobbler/query",
		},
		"help": "Add trailing slash to a group or command path for a description",
	})
}

// sendJSON writes a 200 JSON response.
func sendJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}

// sendError writes an error JSON response with the given HTTP status code.
func sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
