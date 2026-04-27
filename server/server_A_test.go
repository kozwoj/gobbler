package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kozwoj/gobbler/pipeline"
)

// newTestRouter wires the same route tree as ListenAndServe but returns the
// handler without starting a TCP listener. Tests drive it via httptest.
func newTestRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Route("/gobbler", func(r chi.Router) {
		r.Get("/", s.handleRootDiscovery)
		r.Route("/definition", s.definitionRoutes)
		r.Route("/pipeline", s.pipelineRoutes)
		r.Route("/ingest", s.ingestRoutes)
	})
	return r
}

// do is a small helper that fires a request against the test router and returns
// the response recorder so callers can inspect status code and body.
func do(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *strings.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	} else {
		reqBody = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// decodeJSON decodes the response body into a map for assertion.
func decodeJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("could not decode response JSON: %v\nbody: %s", err, w.Body.String())
	}
	return m
}

// ---- Category A: Pre-condition enforcement (before configure/start) ----

// A1: Fresh server status shows not configured and not running.
func TestA1_StatusBeforeConfigure(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeJSON(t, w)
	if body["configured"] != false {
		t.Errorf("expected configured=false, got %v", body["configured"])
	}
	if body["running"] != false {
		t.Errorf("expected running=false, got %v", body["running"])
	}
}

// A2: Start without configure returns 409.
func TestA2_StartWithoutConfigure(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

// A3: Adding a valid definition before configure/start succeeds (200) and the
// server is still not running afterwards.
func TestA3_AddDefinitionBeforeStart(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	alphaDef := `{
		"name": "alpha",
		"documentation": "test definition alpha with string, int and datetime types",
		"folder": "alpha-folder",
		"latencyMinutes": 1,
		"orderedColumns": [
			{"name": "alphaStr",  "type": "string"},
			{"name": "alphaInt",  "type": "int"},
			{"name": "alphaDate", "type": "datetime"}
		]
	}`

	w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}

	// Pipeline must still be not-running.
	if s.IsRunning() {
		t.Error("pipeline should not be running after definition/add alone")
	}
	// Definition must be stored.
	if _, err := s.definitions.GetDefinition("alpha"); err != nil {
		t.Errorf("alpha definition not found after add: %v", err)
	}
}

// A4: Start with a definition registered but still no config returns 409.
func TestA4_StartWithDefinitionButNoConfig(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	alphaDef := `{
		"name": "alpha",
		"documentation": "test definition alpha with string, int and datetime types",
		"folder": "alpha-folder",
		"latencyMinutes": 1,
		"orderedColumns": [
			{"name": "alphaStr",  "type": "string"},
			{"name": "alphaInt",  "type": "int"},
			{"name": "alphaDate", "type": "datetime"}
		]
	}`
	do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// A5: Ingest when pipeline is not running returns 409.
func TestA5_IngestWhenNotRunning(t *testing.T) {
	t.Cleanup(pipeline.Reset)

	s := New()
	router := newTestRouter(s)

	body := `[{"alpha": {"alphaStr": "hello", "alphaInt": 1, "alphaDate": "2026-04-25 10:00:00.000"}}]`
	w := do(t, router, http.MethodPost, "/gobbler/ingest", body)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
