package server

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
)

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

// A6: Definition names returns an empty array when no definitions are registered.
func TestA6_DefinitionNamesEmpty(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodGet, "/gobbler/definition/names", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("could not decode response as []string: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty array, got %v", names)
	}
}

// A7: Definition names returns the registered type names.
func TestA7_DefinitionNamesAfterAdd(t *testing.T) {
	s := New()
	router := newTestRouter(s)
	do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef)
	do(t, router, http.MethodPost, "/gobbler/definition/add", betaDef)

	w := do(t, router, http.MethodGet, "/gobbler/definition/names", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var names []string
	if err := json.NewDecoder(w.Body).Decode(&names); err != nil {
		t.Fatalf("could not decode response as []string: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d: %v", len(names), names)
	}
	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}
	if !nameSet["alpha"] || !nameSet["beta"] {
		t.Errorf("expected alpha and beta in names, got %v", names)
	}
}

// ---- Category B: Configure validation ----

// B1: Configure with missing mode returns 400.
func TestB1_ConfigureMissingMode(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"outputDir": "/tmp/gobbler", "writerQueueSize": 10, "writerBatchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B2: Configure with mode=file but no outputDir returns 400.
func TestB2_ConfigureFileModeNoOutputDir(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "file", "writerQueueSize": 10, "writerBatchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B3: Configure with mode=blob but missing accountKey returns 400.
func TestB3_ConfigureBlobModeNoAccountKey(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "blob", "accountName": "myaccount", "writerQueueSize": 10, "writerBatchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B4: Configure with valid file mode returns 200.
func TestB4_ConfigureFileModeValid(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "file", "outputDir": "/tmp/gobbler", "writerQueueSize": 10, "writerBatchSize": 5}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", body["status"])
	}
}

// B5: Status after valid configure reflects the stored configuration.
func TestB5_StatusAfterConfigure(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "file", "outputDir": "/tmp/gobbler", "writerQueueSize": 10, "writerBatchSize": 5}`)

	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)

	if body["configured"] != true {
		t.Errorf("expected configured=true, got %v", body["configured"])
	}
	if body["running"] != false {
		t.Errorf("expected running=false, got %v", body["running"])
	}
	if body["mode"] != "file" {
		t.Errorf("expected mode=file, got %v", body["mode"])
	}
	if body["writerQueueSize"] != float64(10) {
		t.Errorf("expected writerQueueSize=10, got %v", body["writerQueueSize"])
	}
	if body["writerBatchSize"] != float64(5) {
		t.Errorf("expected writerBatchSize=5, got %v", body["writerBatchSize"])
	}
}
