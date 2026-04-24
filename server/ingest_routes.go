package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) ingestRoutes(r chi.Router) {
	r.Get("/", s.handleIngestDiscovery)
	r.Post("/", s.handleIngest)
}

func (s *Server) handleIngestDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "ingest",
		"description": "Ingest an array of typed items into the pipeline.",
		"method":      "POST",
		"endpoint":    "/gobbler/ingest",
		"input":       `[{"typeName": {...itemFields}}, ...]`,
		"returns":     `{"ingested": N, "rejected": [...]} or {"error": "..."}`,
	})
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}
