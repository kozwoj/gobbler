package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// ---- Category C: Happy path ----

// TestC_HappyPath runs the full configure → add → start → ingest → stop → status cycle
// as a single ordered test.
func TestC_HappyPath(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()

	s := New()
	router := newTestRouter(s)

	// C1 — configure (file mode)
	t.Run("C1_Configure", func(t *testing.T) {
		configureFileMode(t, router, outputDir)
	})

	// C2 — add alpha definition
	t.Run("C2_AddAlpha", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C3 — add beta definition
	t.Run("C3_AddBeta", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/definition/add", betaDef)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C4 — list definitions
	t.Run("C4_ListDefinitions", func(t *testing.T) {
		w := do(t, router, http.MethodGet, "/gobbler/definition/list", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var list []interface{}
		if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
			t.Fatalf("could not decode list response: %v", err)
		}
		if len(list) != 2 {
			t.Errorf("expected 2 definitions, got %d", len(list))
		}
	})

	// C5 — start pipeline; output subdirectories must be created
	t.Run("C5_Start", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		for _, folder := range []string{"alpha-folder", "beta-folder"} {
			if _, err := os.Stat(filepath.Join(outputDir, folder)); os.IsNotExist(err) {
				t.Errorf("expected output subdirectory %s to exist after start", folder)
			}
		}
	})

	// C6 — status immediately after start: running=true, types map present with zeroed stats
	t.Run("C6_StatusAfterStart", func(t *testing.T) {
		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)

		if body["running"] != true {
			t.Errorf("expected running=true, got %v", body["running"])
		}

		writers, ok := body["writers"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected writers map in status, got %T: %v", body["writers"], body["writers"])
		}
		for _, typeName := range []string{"alpha", "beta"} {
			entry, ok := writers[typeName].(map[string]interface{})
			if !ok {
				t.Fatalf("expected writers[%s] to be a map, got %T", typeName, writers[typeName])
			}
			if entry["itemsInBuffer"] != float64(0) {
				t.Errorf("%s: expected itemsInBuffer=0, got %v", typeName, entry["itemsInBuffer"])
			}
			if entry["itemsWritten"] != float64(0) {
				t.Errorf("%s: expected itemsWritten=0, got %v", typeName, entry["itemsWritten"])
			}
			if entry["currentOutput"] != "" {
				t.Errorf("%s: expected currentOutput empty, got %v", typeName, entry["currentOutput"])
			}
		}
	})

	// C7 — ingest 3 alpha and 2 beta items; all should be accepted
	const alphaCount = 3
	const betaCount = 2
	t.Run("C7_Ingest", func(t *testing.T) {
		alphaArray, err := tester.NewAlphaGenerator().GenerateJSONArray(alphaCount)
		if err != nil {
			t.Fatalf("generate alpha: %v", err)
		}
		betaArray, err := tester.NewBetaGenerator().GenerateJSONArray(betaCount)
		if err != nil {
			t.Fatalf("generate beta: %v", err)
		}
		// Merge into a single JSON array by stripping the outer brackets and rejoining.
		batch := alphaArray[:len(alphaArray)-1] + "," + betaArray[1:]
		w := do(t, router, http.MethodPost, "/gobbler/ingest", batch)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(alphaCount+betaCount) {
			t.Errorf("expected ingested=%d, got %v", alphaCount+betaCount, body["ingested"])
		}
		rejected, _ := body["rejected"].([]interface{})
		if len(rejected) != 0 {
			t.Errorf("expected no rejected items, got %v", rejected)
		}
	})

	// C8 — wait for flush tick (up to 2 s) then check itemsWritten in status
	t.Run("C8_StatsAfterFlush", func(t *testing.T) {
		deadline := time.Now().Add(2 * time.Second)
		var alphaWritten, betaWritten float64
		for time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
			body := decodeJSON(t, w)
			writers, ok := body["writers"].(map[string]interface{})
			if !ok {
				continue
			}
			alphaEntry, _ := writers["alpha"].(map[string]interface{})
			betaEntry, _ := writers["beta"].(map[string]interface{})
			alphaWritten, _ = alphaEntry["itemsWritten"].(float64)
			betaWritten, _ = betaEntry["itemsWritten"].(float64)
			if alphaWritten == float64(alphaCount) && betaWritten == float64(betaCount) {
				break
			}
		}
		if alphaWritten != float64(alphaCount) {
			t.Errorf("expected alpha.itemsWritten=%d, got %v", alphaCount, alphaWritten)
		}
		if betaWritten != float64(betaCount) {
			t.Errorf("expected beta.itemsWritten=%d, got %v", betaCount, betaWritten)
		}
	})

	// C9 — CSV files exist on disk
	t.Run("C9_FilesOnDisk", func(t *testing.T) {
		for _, folder := range []string{"alpha-folder", "beta-folder"} {
			entries, err := os.ReadDir(filepath.Join(outputDir, folder))
			if err != nil {
				t.Fatalf("could not read output dir %s: %v", folder, err)
			}
			if len(entries) == 0 {
				t.Errorf("expected at least one CSV file in %s, found none", folder)
			}
		}
	})

	// C10 — stop pipeline
	t.Run("C10_Stop", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C11 — status after stop: running=false, configured=true, no writers key
	t.Run("C11_StatusAfterStop", func(t *testing.T) {
		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)

		if body["running"] != false {
			t.Errorf("expected running=false after stop, got %v", body["running"])
		}
		if body["configured"] != true {
			t.Errorf("expected configured=true after stop, got %v", body["configured"])
		}
		if _, present := body["writers"]; present {
			t.Errorf("expected writers key to be absent after stop, but it was present")
		}
	})
}

// ---- Category E: Hot-add and hot-remove while running ----

// TestE_HotAddRemove runs E1–E5 as a single ordered test.
func TestE_HotAddRemove(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)

	// E1 — start with only alpha; writers map must have exactly alpha.
	t.Run("E1_StartAlphaOnly", func(t *testing.T) {
		configureFileMode(t, router, outputDir)
		if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
			t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
		}
		if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
			t.Fatalf("start: %d %s", w.Code, w.Body.String())
		}

		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, ok := body["writers"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected writers map in status, got %T", body["writers"])
		}
		if _, hasAlpha := writers["alpha"]; !hasAlpha {
			t.Errorf("expected alpha in writers map")
		}
		if _, hasGamma := writers["gamma"]; hasGamma {
			t.Errorf("gamma should not be present before hot-add")
		}
	})

	// E2 — hot-add gamma while running; status must include gamma with zeroed stats.
	t.Run("E2_HotAddGamma", func(t *testing.T) {
		if w := do(t, router, http.MethodPost, "/gobbler/definition/add", gammaDef); w.Code != http.StatusOK {
			t.Fatalf("hot-add gamma: %d %s", w.Code, w.Body.String())
		}

		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, ok := body["writers"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected writers map after hot-add, got %T", body["writers"])
		}
		gammaEntry, ok := writers["gamma"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected gamma entry in writers map after hot-add")
		}
		if gammaEntry["itemsWritten"] != float64(0) {
			t.Errorf("expected gamma.itemsWritten=0 after hot-add, got %v", gammaEntry["itemsWritten"])
		}
		if gammaEntry["itemsInBuffer"] != float64(0) {
			t.Errorf("expected gamma.itemsInBuffer=0 after hot-add, got %v", gammaEntry["itemsInBuffer"])
		}
	})

	// E3 — ingest N gamma items; after flush tick itemsWritten must equal N.
	const gammaCount = 3
	t.Run("E3_IngestGamma", func(t *testing.T) {
		gammaArray, err := tester.NewGammaGenerator().GenerateJSONArray(gammaCount)
		if err != nil {
			t.Fatalf("generate gamma: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", gammaArray)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest gamma: %d %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(gammaCount) {
			t.Errorf("expected ingested=%d, got %v", gammaCount, body["ingested"])
		}

		written := waitForWritten(t, router, "gamma", gammaCount)
		if written != float64(gammaCount) {
			t.Errorf("expected gamma.itemsWritten=%d after flush, got %v", gammaCount, written)
		}
	})

	// E4 — hot-remove gamma; it must disappear from status writers and its file must be flushed.
	t.Run("E4_HotRemoveGamma", func(t *testing.T) {
		if w := do(t, router, http.MethodPost, "/gobbler/definition/remove", `{"typeName":"gamma"}`); w.Code != http.StatusOK {
			t.Fatalf("hot-remove gamma: %d %s", w.Code, w.Body.String())
		}

		// gamma must be absent from writers.
		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, _ := body["writers"].(map[string]interface{})
		if _, stillPresent := writers["gamma"]; stillPresent {
			t.Errorf("gamma should be gone from writers after hot-remove")
		}

		// gamma's file must be on disk (flushed and closed by cancel+Wait).
		entries, err := os.ReadDir(filepath.Join(outputDir, "gamma-folder"))
		if err != nil {
			t.Fatalf("could not read gamma-folder: %v", err)
		}
		if len(entries) == 0 {
			t.Errorf("expected gamma CSV file on disk after hot-remove, found none")
		}
	})

	// E5 — ingesting gamma after removal lands everything in rejected.
	t.Run("E5_IngestAfterRemove", func(t *testing.T) {
		gammaArray, err := tester.NewGammaGenerator().GenerateJSONArray(1)
		if err != nil {
			t.Fatalf("generate gamma: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", gammaArray)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(0) {
			t.Errorf("expected ingested=0 after remove, got %v", body["ingested"])
		}
		rejected, _ := body["rejected"].([]interface{})
		if len(rejected) != 1 {
			t.Errorf("expected 1 rejected entry, got %d", len(rejected))
		}
	})

	// cleanup — stop the pipeline so writers are closed before t.TempDir is removed.
	do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")
}

// ---- Category F: Rotate ----

// TestF_Rotate exercises F1–F3 as a single ordered test.
func TestF_Rotate(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)

	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// F1 — ingest 3 items (well below batchSize=50); status must show itemsInBuffer > 0
	// and currentOutput == "" because no flush has happened yet.
	t.Run("F1_BufferedBeforeFlush", func(t *testing.T) {
		f1Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(3)
		if err != nil {
			t.Fatalf("generate alpha: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", f1Batch)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}

		// Sleep briefly so pipeline goroutines can deliver items to the writer
		// buffer. 50 ms is well within the 500 ms tick interval so no flush
		// should have occurred yet.
		time.Sleep(50 * time.Millisecond)
		status := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, status)
		writers, _ := body["writers"].(map[string]interface{})
		alphaEntry, _ := writers["alpha"].(map[string]interface{})

		if inBuf, _ := alphaEntry["itemsInBuffer"].(float64); inBuf == 0 {
			t.Errorf("expected itemsInBuffer > 0 before flush, got 0")
		}
		if cur, _ := alphaEntry["currentOutput"].(string); cur != "" {
			t.Errorf("expected currentOutput empty before flush, got %q", cur)
		}
	})

	// F2 — rotate alpha; buffer must be flushed and file closed.
	t.Run("F2_RotateFlushesAndCloses", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/pipeline/rotate", `{"typeName":"alpha"}`)
		if w.Code != http.StatusOK {
			t.Fatalf("rotate: %d %s", w.Code, w.Body.String())
		}

		status := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, status)
		writers, _ := body["writers"].(map[string]interface{})
		alphaEntry, _ := writers["alpha"].(map[string]interface{})

		if inBuf, _ := alphaEntry["itemsInBuffer"].(float64); inBuf != 0 {
			t.Errorf("expected itemsInBuffer=0 after rotate, got %v", inBuf)
		}
		written, _ := alphaEntry["itemsWritten"].(float64)
		if written == 0 {
			t.Errorf("expected itemsWritten > 0 after rotate, got 0")
		}
		// Rotate closes the file, so currentOutput must be empty.
		if cur, _ := alphaEntry["currentOutput"].(string); cur != "" {
			t.Errorf("expected currentOutput empty after rotate (file closed), got %q", cur)
		}
	})

	// F3 — ingest more items and wait for the flush tick; a second CSV file must
	// appear in alpha-folder (the first was written by the rotate in F2).
	t.Run("F3_SecondFileAfterRotate", func(t *testing.T) {
		f3Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(2)
		if err != nil {
			t.Fatalf("generate alpha: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", f3Batch)
		if w.Code != http.StatusOK {
			t.Fatalf("second ingest: %d %s", w.Code, w.Body.String())
		}

		// Wait up to 3 s for the tick to flush the second batch.
		alphaDir := filepath.Join(outputDir, "alpha-folder")
		deadline := time.Now().Add(3 * time.Second)
		var fileCount int
		for time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			entries, err := os.ReadDir(alphaDir)
			if err != nil {
				t.Fatalf("could not read alpha-folder: %v", err)
			}
			fileCount = len(entries)
			if fileCount >= 2 {
				break
			}
		}
		if fileCount < 2 {
			t.Errorf("expected at least 2 CSV files in alpha-folder after rotate+re-ingest, got %d", fileCount)
		}
	})
}

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
	// dir1/alpha-folder must have no CSV data files (no ingest happened in cycle 1).
	entries1, _ := os.ReadDir(filepath.Join(dir1, "alpha-folder"))
	csvCount := 0
	for _, e := range entries1 {
		if strings.HasSuffix(e.Name(), ".csv") {
			csvCount++
		}
	}
	if csvCount > 0 {
		t.Errorf("expected no CSV files in dir1/alpha-folder after reconfigure, got %d", csvCount)
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
