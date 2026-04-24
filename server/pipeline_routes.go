package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePipelineStart(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePipelineStop(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePipelineRotate(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
}

func (s *Server) handlePipelineStatus(w http.ResponseWriter, r *http.Request) {
	sendError(w, http.StatusNotImplemented, "not implemented")
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
