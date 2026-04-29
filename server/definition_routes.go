package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
)

func (s *Server) definitionRoutes(r chi.Router) {
	r.Get("/", s.handleDefinitionDiscovery)

	r.Post("/add", s.handleDefinitionAdd)
	r.Get("/add/", s.handleDefinitionAddHelp)

	r.Get("/list", s.handleDefinitionList)
	r.Get("/list/", s.handleDefinitionListHelp)

	r.Get("/names", s.handleDefinitionNames)
	r.Get("/names/", s.handleDefinitionNamesHelp)

	r.Post("/remove", s.handleDefinitionRemove)
	r.Get("/remove/", s.handleDefinitionRemoveHelp)
}

func (s *Server) handleDefinitionDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Item type definition management",
		"available_routes": []string{
			"/gobbler/definition/add",
			"/gobbler/definition/list",
			"/gobbler/definition/names",
			"/gobbler/definition/remove",
		},
		"help": "Add trailing slash to a command path for details",
	})
}

func (s *Server) handleDefinitionAdd(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "could not read request body")
		return
	}

	var def items.ItemDefinition
	if err := items.CreateItemDefinition(string(body), &def); err != nil {
		sendError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.definitions.AddDefinition(def); err != nil {
		sendError(w, http.StatusConflict, err.Error())
		return
	}

	if s.running {
		if err := s.startType(def); err != nil {
			// Undo the definition registration so state stays consistent.
			s.definitions.RemoveDefinition(def.TypeName)
			sendError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	sendJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDefinitionList(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]items.ItemDefinition, 0, len(s.definitions))
	for _, def := range s.definitions {
		list = append(list, def)
	}
	sendJSON(w, list)
}

// handleDefinitionNames returns a lightweight JSON array of registered type
// name strings. Intended for clients that only need to verify which types are
// known without parsing full definitions.
func (s *Server) handleDefinitionNames(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	names := make([]string, 0, len(s.definitions))
	for name := range s.definitions {
		names = append(names, name)
	}
	sendJSON(w, names)
}

func (s *Server) handleDefinitionRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TypeName string `json:"typeName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TypeName == "" {
		sendError(w, http.StatusBadRequest, "typeName is required")
		return
	}

	s.mu.Lock()

	if _, err := s.definitions.GetDefinition(req.TypeName); err != nil {
		s.mu.Unlock()
		sendError(w, http.StatusNotFound, err.Error())
		return
	}

	var entry *typeEntry
	if s.running {
		t := pipeline.ItemType(req.TypeName)
		if e, ok := s.types[t]; ok {
			// Remove from routing table first so no new items are routed here.
			pipeline.RemoveItemType(t)
			delete(s.types, t)
			entry = e
		}
	}

	s.definitions.RemoveDefinition(req.TypeName)
	s.mu.Unlock()

	// Stop the type's goroutines outside the lock.
	if entry != nil {
		entry.cancel()
		entry.wg.Wait()
	}

	sendJSON(w, map[string]string{"status": "ok"})
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

func (s *Server) handleDefinitionNamesHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "definition/names",
		"description": "Returns a lightweight array of registered item type name strings without full definition details.",
		"method":      "GET",
		"endpoint":    "/gobbler/definition/names",
		"input":       "none",
		"returns":     `["name1", "name2", ...]`,
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
