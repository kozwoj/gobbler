package server

import (
	"net/http"
	"testing"
)

// ---- Category B: Configure validation ----

// B1: Configure with missing mode returns 400.
func TestB1_ConfigureMissingMode(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"outputDir": "/tmp/gobbler", "workerQueueSize": 10, "batchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B2: Configure with mode=file but no outputDir returns 400.
func TestB2_ConfigureFileModeNoOutputDir(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "file", "workerQueueSize": 10, "batchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B3: Configure with mode=blob but missing accountKey returns 400.
func TestB3_ConfigureBlobModeNoAccountKey(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "blob", "accountName": "myaccount", "workerQueueSize": 10, "batchSize": 5}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// B4: Configure with valid file mode returns 200.
func TestB4_ConfigureFileModeValid(t *testing.T) {
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure",
		`{"mode": "file", "outputDir": "/tmp/gobbler", "workerQueueSize": 10, "batchSize": 5}`)

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
		`{"mode": "file", "outputDir": "/tmp/gobbler", "workerQueueSize": 10, "batchSize": 5}`)

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
	if body["workerQueueSize"] != float64(10) {
		t.Errorf("expected workerQueueSize=10, got %v", body["workerQueueSize"])
	}
	if body["batchSize"] != float64(5) {
		t.Errorf("expected batchSize=5, got %v", body["batchSize"])
	}
}
