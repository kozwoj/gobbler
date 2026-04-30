package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// alphaDef and betaDef are the JSON item type definitions used across Category C tests.
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

// ---- Category C: Happy path ----

// TestC_HappyPath runs the full configure → add → start → ingest → stop → status cycle
// as a single ordered test, mirroring the sequence in test_notes.md C1–C11.
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

	// C11 — status after stop: running=false, configured=true, no types key
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
