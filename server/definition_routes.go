package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) definitionRoutes(r chi.Router) {
	r.Get("/", s.handleDefinitionDiscovery)

	r.Post("/add", s.handleDefinitionAdd)
	r.Get("/add/", s.handleDefinitionAddHelp)

	r.Get("/list", s.handleDefinitionList)
	r.Get("/list/", s.handleDefinitionListHelp)

	r.Post("/remove", s.handleDefinitionRemove)
	r.Get("/remove/", s.handleDefinitionRemoveHelp)
}

func (s *Server) handleDefinitionDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Item type definition management",
		"available_routes": []string{
			"/gobbler/definition/add",
			"/gobbler/definition/list",
			"/gobbler/definition/remove",
		},
		"help": "Add trailing slash to a command path for details",
	})
}

func (s *Server) handleDefinitionAdd(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleDefinitionList(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleDefinitionRemove(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handleDefinitionAddHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "definition/add",
		"description": "Parses, validates and registers a new item type definition. If the pipeline is running, also creates and starts the writer and worker for the new type.",
		"method":      "POST",
		"endpoint":    "/gobbler/definition/add",
		"input":       "item definition JSON object (see docs/item_schema.json)",
		"returns":     `{"status": "ok"} or {"error": "..."}`,
	})
}

func (s *Server) handleDefinitionListHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "definition/list",
		"description": "Returns all currently registered item type definitions.",
		"method":      "GET",
		"endpoint":    "/gobbler/definition/list",
		"input":       "none",
		"returns":     "array of item definition objects",
	})
}

func (s *Server) handleDefinitionRemoveHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "definition/remove",
		"description": "Removes an item type definition. If the pipeline is running, stops the corresponding writer (flushing its buffer) and removes the type from the routing table.",
		"method":      "POST",
		"endpoint":    "/gobbler/definition/remove",
		"input":       `{"typeName": "string"}`,
		"returns":     `{"status": "ok"} or {"error": "..."}`,
	})
}
