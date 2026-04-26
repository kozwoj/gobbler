package server

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		sendError(w, http.StatusBadRequest, "could not read request body")
		return
	}

	s.mu.RLock()
	running := s.running
	// Snapshot definitions so we can release the lock before conversion work.
	var defsCopy items.DefinitionList
	if running {
		defsCopy = make(items.DefinitionList, len(s.definitions))
		for k, v := range s.definitions {
			defsCopy[k] = v
		}
	}
	s.mu.RUnlock()

	if !running {
		sendError(w, http.StatusConflict, "pipeline is not running")
		return
	}

	inputItems, parseErrors := items.SplitInput(body)
	timestamp := time.Now().UTC().Format("2006-01-02 15:04:05.000")

	var rejected []map[string]interface{}
	for _, pe := range parseErrors {
		rejected = append(rejected, map[string]interface{}{"error": pe.Error()})
	}

	ingested := 0
	for _, item := range inputItems {
		csvStr, errs := items.ConvertItem(item, defsCopy, timestamp)
		if len(errs) > 0 {
			errMsgs := make([]string, len(errs))
			for i, e := range errs {
				errMsgs[i] = e.Error()
			}
			rejected = append(rejected, map[string]interface{}{
				"typeName": item.ItemTypeName,
				"errors":   errMsgs,
			})
			continue
		}

		desc := pipeline.LookupType(pipeline.ItemType(item.ItemTypeName))
		if desc == nil {
			rejected = append(rejected, map[string]interface{}{
				"typeName": item.ItemTypeName,
				"error":    "type not registered",
			})
			continue
		}
		select {
		case desc.Queue <- pipeline.CSVitem{Type: pipeline.ItemType(item.ItemTypeName), CSV: csvStr}:
			// delivered directly to writer's worker queue
		default:
			rejected = append(rejected, map[string]interface{}{
				"typeName": item.ItemTypeName,
				"error":    "worker queue full",
			})
			continue
		}
		ingested++
	}

	sendJSON(w, map[string]interface{}{
		"ingested": ingested,
		"rejected": rejected,
	})
}
