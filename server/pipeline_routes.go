package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/kozwoj/gobbler/pipeline"
)

func (s *Server) pipelineRoutes(r chi.Router) {
	r.Get("/", s.handlePipelineDiscovery)

	r.Post("/configure", s.handlePipelineConfigure)
	r.Get("/configure/", s.handlePipelineConfigureHelp)

	r.Post("/start", s.handlePipelineStart)
	r.Get("/start/", s.handlePipelineStartHelp)

	r.Post("/stop", s.handlePipelineStop)
	r.Get("/stop/", s.handlePipelineStopHelp)

	r.Post("/rotate", s.handlePipelineRotate)
	r.Get("/rotate/", s.handlePipelineRotateHelp)

	r.Get("/status", s.handlePipelineStatus)
	r.Get("/status/", s.handlePipelineStatusHelp)
}

func (s *Server) handlePipelineDiscovery(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"description": "Pipeline lifecycle management",
		"available_routes": []string{
			"/gobbler/pipeline/configure",
			"/gobbler/pipeline/start",
			"/gobbler/pipeline/stop",
			"/gobbler/pipeline/rotate",
			"/gobbler/pipeline/status",
		},
		"help": "Add trailing slash to a command path for details",
	})
}

func (s *Server) handlePipelineConfigure(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode             string `json:"mode"`
		OutputDir        string `json:"outputDir"`
		AccountName      string `json:"accountName"`
		AccountKey       string `json:"accountKey"`
		CentralQueueSize int    `json:"centralQueueSize"`
		WorkerQueueSize  int    `json:"workerQueueSize"`
		BatchSize        int    `json:"batchSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	switch pipeline.StorageMode(req.Mode) {
	case pipeline.StorageModeFile:
		if req.OutputDir == "" {
			sendError(w, http.StatusBadRequest, "outputDir is required for file mode")
			return
		}
	case pipeline.StorageModeBlob:
		if req.AccountName == "" || req.AccountKey == "" {
			sendError(w, http.StatusBadRequest, "accountName and accountKey are required for blob mode")
			return
		}
	default:
		sendError(w, http.StatusBadRequest, `mode must be "file" or "blob"`)
		return
	}

	cfg := &pipeline.Config{
		Mode:             pipeline.StorageMode(req.Mode),
		OutputDir:        req.OutputDir,
		AccountName:      req.AccountName,
		AccountKey:       req.AccountKey,
		CentralQueueSize: req.CentralQueueSize,
		WorkerQueueSize:  req.WorkerQueueSize,
		BatchSize:        req.BatchSize,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		sendError(w, http.StatusConflict, "cannot reconfigure while pipeline is running; stop it first")
		return
	}

	s.config = cfg
	sendJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePipelineStart(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.config == nil {
		sendError(w, http.StatusConflict, "pipeline not configured; call pipeline/configure first")
		return
	}
	if s.running {
		sendError(w, http.StatusConflict, "pipeline is already running")
		return
	}
	if len(s.definitions) == 0 {
		sendError(w, http.StatusConflict, "no item type definitions registered; call definition/add first")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.pipelineCtx = ctx
	s.cancel = cancel

	pipeline.Start(ctx, &s.wg, s.config.CentralQueueSize)

	for _, def := range s.definitions {
		if err := s.startType(def); err != nil {
			// Roll back: cancel all goroutines started so far, then reset.
			cancel()
			s.wg.Wait()
			for _, entry := range s.types {
				entry.wg.Wait()
			}
			pipeline.Reset()
			s.pipelineCtx = nil
			s.cancel = nil
			s.types = make(map[pipeline.ItemType]*typeEntry)
			sendError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.running = true
	sendJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePipelineStop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()

	if !s.running {
		s.mu.Unlock()
		sendError(w, http.StatusConflict, "pipeline is not running")
		return
	}

	// Mark stopped and capture state before releasing the lock.
	s.running = false
	cancel := s.cancel
	s.cancel = nil
	s.pipelineCtx = nil
	types := s.types
	s.types = make(map[pipeline.ItemType]*typeEntry)
	s.mu.Unlock()

	// Cancel and drain all goroutines outside the lock.
	cancel()
	s.wg.Wait()
	for _, entry := range types {
		entry.wg.Wait()
	}
	pipeline.Reset()

	sendJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePipelineRotate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TypeName string `json:"typeName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TypeName == "" {
		sendError(w, http.StatusBadRequest, "typeName is required")
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		sendError(w, http.StatusConflict, "pipeline is not running")
		return
	}

	entry, ok := s.types[pipeline.ItemType(req.TypeName)]
	if !ok {
		sendError(w, http.StatusNotFound, "unknown type: "+req.TypeName)
		return
	}

	entry.writer.Rotate()
	sendJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]interface{}{
		"configured":            s.config != nil,
		"running":               s.running,
		"registeredDefinitions": len(s.definitions),
	}

	if s.config != nil {
		status["mode"] = string(s.config.Mode)
		status["centralQueueSize"] = s.config.CentralQueueSize
		status["workerQueueSize"] = s.config.WorkerQueueSize
		status["batchSize"] = s.config.BatchSize
	}

	if s.running {
		typeStats := make(map[string]interface{}, len(s.types))
		for t, entry := range s.types {
			st := entry.writer.Stats()
			typeStats[string(t)] = map[string]interface{}{
				"itemsInBuffer": st.ItemsInBuffer,
				"itemsWritten":  st.ItemsWritten,
				"lastFlush":     st.LastFlush,
				"currentOutput": st.CurrentOutput,
			}
		}
		status["writers"] = typeStats
	}

	sendJSON(w, status)
}

func (s *Server) handlePipelineConfigureHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "pipeline/configure",
		"description": "Sets pipeline storage mode, queue sizes and writer batch size. Must be called before pipeline/start.",
		"method":      "POST",
		"endpoint":    "/gobbler/pipeline/configure",
		"input": map[string]string{
			"mode":             `"file" or "blob"`,
			"outputDir":        "string - required when mode is \"file\"",
			"accountName":      "string - required when mode is \"blob\"",
			"accountKey":       "string - required when mode is \"blob\"",
			"centralQueueSize": "integer",
			"workerQueueSize":  "integer",
			"batchSize":        "integer",
		},
		"returns": `{"status": "ok"} or {"error": "..."}`,
	})
}

func (s *Server) handlePipelineStartHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "pipeline/start",
		"description": "Starts the pipeline. Requires pipeline/configure to have been called and at least one item type definition to be registered.",
		"method":      "POST",
		"endpoint":    "/gobbler/pipeline/start",
		"input":       "none",
		"returns":     `{"status": "ok"} or {"error": "..."}`,
	})
}

func (s *Server) handlePipelineStopHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "pipeline/stop",
		"description": "Stops the pipeline; flushes and closes all writers. The pipeline can be reconfigured and restarted after.",
		"method":      "POST",
		"endpoint":    "/gobbler/pipeline/stop",
		"input":       "none",
		"returns":     `{"status": "ok"} or {"error": "..."}`,
	})
}

func (s *Server) handlePipelineRotateHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "pipeline/rotate",
		"description": "Flushes and rotates the active file or blob for the specified item type.",
		"method":      "POST",
		"endpoint":    "/gobbler/pipeline/rotate",
		"input":       `{"typeName": "string"}`,
		"returns":     `{"status": "ok"} or {"error": "..."}`,
	})
}

func (s *Server) handlePipelineStatusHelp(w http.ResponseWriter, r *http.Request) {
	sendJSON(w, map[string]interface{}{
		"command":     "pipeline/status",
		"description": "Returns current pipeline status and basic statistics.",
		"method":      "GET",
		"endpoint":    "/gobbler/pipeline/status",
		"input":       "none",
		"returns":     "pipeline status object",
	})
}
