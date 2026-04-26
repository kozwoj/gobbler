package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
)

const gammaDef = `{
	"name": "gamma",
	"documentation": "test definition gamma with int, string, and dynamic types",
	"folder": "gammaFolder",
	"latencyMinutes": 3,
	"orderedColumns": [
		{"name": "gammaInt",     "type": "int"},
		{"name": "gammaStr",     "type": "string"},
		{"name": "gammaDynamic", "type": "dynamic"}
	]
}`

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
		w := do(t, router, http.MethodPost, "/gobbler/ingest", `[
			{"gamma": {"gammaInt": 1, "gammaStr": "a", "gammaDynamic": "{\"k\":1}"}},
			{"gamma": {"gammaInt": 2, "gammaStr": "b", "gammaDynamic": "{\"k\":2}"}},
			{"gamma": {"gammaInt": 3, "gammaStr": "c", "gammaDynamic": "{\"k\":3}"}}
		]`)
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
		entries, err := os.ReadDir(filepath.Join(outputDir, "gammaFolder"))
		if err != nil {
			t.Fatalf("could not read gammaFolder: %v", err)
		}
		if len(entries) == 0 {
			t.Errorf("expected gamma CSV file on disk after hot-remove, found none")
		}
	})

	// E5 — ingesting gamma after removal lands everything in rejected.
	t.Run("E5_IngestAfterRemove", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/ingest", `[
			{"gamma": {"gammaInt": 9, "gammaStr": "z", "gammaDynamic": "{\"k\":9}"}}
		]`)
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
