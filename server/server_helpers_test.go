package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// ---- shared item type definition fixtures ----

const alphaDef = `{
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

const betaDef = `{
	"name": "beta",
	"documentation": "test definition beta with string, bool and real types",
	"folder": "beta-folder",
	"latencyMinutes": 2,
	"orderedColumns": [
		{"name": "betaStr",  "type": "string"},
		{"name": "betaBool", "type": "bool"},
		{"name": "betaReal", "type": "real"}
	]
}`

const gammaDef = `{
	"name": "gamma",
	"documentation": "test definition gamma with int, string, and dynamic types",
	"folder": "gamma-folder",
	"latencyMinutes": 3,
	"orderedColumns": [
		{"name": "gammaInt",     "type": "int"},
		{"name": "gammaStr",     "type": "string"},
		{"name": "gammaDynamic", "type": "dynamic"}
	]
}`

// ---- HTTP helpers ----

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

// do fires a request against the test router and returns the response recorder.
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

// ---- pipeline setup helpers ----

// configureFileMode posts a valid file-mode configure request using the given outputDir.
func configureFileMode(t *testing.T, router http.Handler, outputDir string) {
	t.Helper()
	cfgBytes, _ := json.Marshal(map[string]interface{}{
		"mode":            "file",
		"outputDir":       outputDir,
		"writerQueueSize": 200,
		"writerBatchSize": 50,
	})
	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfgBytes))
	if w.Code != http.StatusOK {
		t.Fatalf("configure failed: %d %s", w.Code, w.Body.String())
	}
}

// startWithAlpha configures file mode, adds the alpha definition, and starts
// the pipeline. It registers pipeline.Reset as a cleanup and returns the
// router. Tests that call this must stop the pipeline themselves before the
// cleanup runs (or rely on the forced reset for goroutine hygiene).
func startWithAlpha(t *testing.T) http.Handler {
	t.Helper()
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)
	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha failed: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	return router
}

// waitForWritten polls the status endpoint until the named writer reports at
// least wantWritten items written, or the 2-second deadline is exceeded.
// Returns the final itemsWritten value observed.
func waitForWritten(t *testing.T, router http.Handler, typeName string, wantWritten float64) float64 {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var got float64
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, _ := body["writers"].(map[string]interface{})
		entry, _ := writers[typeName].(map[string]interface{})
		got, _ = entry["itemsWritten"].(float64)
		if got >= wantWritten {
			return got
		}
	}
	return got
}

// alphaJSON returns a generated alpha ingest payload for n items, fatal on error.
func alphaJSON(t *testing.T, n int) string {
	t.Helper()
	s, err := tester.NewAlphaGenerator().GenerateJSONArray(n)
	if err != nil {
		t.Fatalf("alphaJSON: %v", err)
	}
	return s
}

// ---- spy logger ----

type logCall struct {
	typeName string
	fields   map[string]any
}

type spyClient struct {
	mu   sync.Mutex
	logs []logCall
}

func (s *spyClient) Log(typeName string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logCall{typeName: typeName, fields: fields})
	return nil
}
func (s *spyClient) Flush(context.Context) error { return nil }
func (s *spyClient) Close(context.Context) error { return nil }
func (s *spyClient) SwapServer(string) error     { return nil }

func (s *spyClient) last() (logCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.logs) == 0 {
		return logCall{}, false
	}
	return s.logs[len(s.logs)-1], true
}

// startWithAlphaSpy starts the pipeline with the alpha definition, injects a
// spyClient into s.logger, and returns the router, the server, and the spy.
func startWithAlphaSpy(t *testing.T) (http.Handler, *Server, *spyClient) {
	t.Helper()
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)
	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha failed: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	spy := &spyClient{}
	// Direct field injection is valid here because tests are in package server.
	s.logger = spy
	return router, s, spy
}
