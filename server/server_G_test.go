package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// ---- Category G: Lifecycle edge cases ----

// G1: Start when already running → 409.
func TestG1_StartWhenRunning(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// G2: Configure when running → 409.
func TestG2_ConfigureWhenRunning(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	cfgBytes, _ := json.Marshal(map[string]interface{}{
		"mode":            "file",
		"outputDir":       t.TempDir(),
		"workerQueueSize": 10,
		"batchSize":       50,
	})
	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfgBytes))
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// G3: Stop when not running → 409.
func TestG3_StopWhenNotRunning(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// G4: Add definition with duplicate name → 409.
func TestG4_AddDuplicateDefinition(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	s := New()
	router := newTestRouter(s)

	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("first add failed: %d %s", w.Code, w.Body.String())
	}
	w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef)
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 on duplicate, got %d: %s", w.Code, w.Body.String())
	}
}

// G5: Remove definition that does not exist → 404.
func TestG5_RemoveNonExistentDefinition(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/definition/remove", `{"typeName":"nonexistent"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// G6: Stop → reconfigure with a different outputDir → start.
// Definitions persist across stop; writes must land in the new directory.
func TestG6_StopReconfigureRestart(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	s := New()
	router := newTestRouter(s)

	// First full cycle in dir1 — no ingest, so dir1/alpha-folder stays empty.
	configureFileMode(t, router, dir1)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha (cycle 1): %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start (cycle 1): %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stop (cycle 1): %d %s", w.Code, w.Body.String())
	}

	// Reconfigure to dir2.
	cfg2, _ := json.Marshal(map[string]interface{}{
		"mode":            "file",
		"outputDir":       dir2,
		"workerQueueSize": 10,
		"batchSize":       50,
	})
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfg2)); w.Code != http.StatusOK {
		t.Fatalf("reconfigure: %d %s", w.Code, w.Body.String())
	}
	// Definitions persist across stop — no need to re-add alpha.
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start (cycle 2): %d %s", w.Code, w.Body.String())
	}

	// Ingest one item and wait for flush.
	g6Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(1)
	if err != nil {
		t.Fatalf("generate alpha: %v", err)
	}
	do(t, router, http.MethodPost, "/gobbler/ingest", g6Batch)
	waitForWritten(t, router, "alpha", 1)

	do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// Files must exist in dir2/alpha-folder.
	entries2, err := os.ReadDir(filepath.Join(dir2, "alpha-folder"))
	if err != nil || len(entries2) == 0 {
		t.Errorf("expected CSV files in dir2/alpha-folder, got err=%v count=%d", err, len(entries2))
	}
	// dir1/alpha-folder must be empty (no ingest happened in cycle 1).
	entries1, _ := os.ReadDir(filepath.Join(dir1, "alpha-folder"))
	if len(entries1) > 0 {
		t.Errorf("expected no CSV files in dir1/alpha-folder after reconfigure, got %d", len(entries1))
	}
}

// G7: Status after stop shows running=false and no writers key.
func TestG7_StatusAfterStop(t *testing.T) {
	router := startWithAlpha(t)
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", w.Code, w.Body.String())
	}

	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["running"] != false {
		t.Errorf("expected running=false, got %v", body["running"])
	}
	if _, present := body["writers"]; present {
		t.Errorf("expected writers key absent after stop")
	}
}
