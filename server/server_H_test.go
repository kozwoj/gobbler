package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// alphaJSON returns a generated alpha ingest payload for n items, fatal on error.
func alphaJSON(t *testing.T, n int) string {
	t.Helper()
	s, err := tester.NewAlphaGenerator().GenerateJSONArray(n)
	if err != nil {
		t.Fatalf("alphaJSON: %v", err)
	}
	return s
}

// ---- Category H: Writer stats accuracy ----

// TestH_StatsAccuracy runs H1–H4 verifying that status.writers["alpha"].itemsWritten
// always matches the cumulative ingested count reported by the ingest endpoint.
func TestH_StatsAccuracy(t *testing.T) {
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

	var cumulative float64

	// H1 — ingest 10 items (below batchSize=50); wait for flush tick; itemsWritten == 10.
	t.Run("H1_TenItems", func(t *testing.T) {
		const n = 10
		w := do(t, router, http.MethodPost, "/gobbler/ingest", alphaJSON(t, n))
		if w.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(n) {
			t.Fatalf("expected ingested=%d, got %v", n, body["ingested"])
		}

		cumulative += n
		written := waitForWritten(t, router, "alpha", cumulative)
		if written != cumulative {
			t.Errorf("expected itemsWritten=%.0f, got %.0f", cumulative, written)
		}
	})

	// H3 — mixed batch: 5 valid alpha + 3 invalid (wrong type for alphaInt).
	// ingested must be 5, rejected must be 3, and itemsWritten increases by exactly 5.
	t.Run("H3_MixedBatch", func(t *testing.T) {
		const validCount = 5
		const invalidCount = 3
		valid := alphaJSON(t, validCount)
		// Build 3 items where alphaInt is a string (invalid type) — must stay hand-coded.
		invalidItems := make([]string, invalidCount)
		for i := range invalidItems {
			invalidItems[i] = fmt.Sprintf(
				`{"alpha":{"alphaStr":"bad%d","alphaInt":"notanint","alphaDate":"2026-04-25 10:00:00.000"}}`, i,
			)
		}
		invalid := strings.Join(invalidItems, ",")
		// Merge into one array: strip the trailing ']' from valid and prepend to invalid.
		batch := valid[:len(valid)-1] + "," + invalid + "]"

		w := do(t, router, http.MethodPost, "/gobbler/ingest", batch)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(validCount) {
			t.Errorf("expected ingested=%d, got %v", validCount, body["ingested"])
		}
		rejected, _ := body["rejected"].([]interface{})
		if len(rejected) != invalidCount {
			t.Errorf("expected %d rejected, got %d", invalidCount, len(rejected))
		}

		cumulative += validCount
		written := waitForWritten(t, router, "alpha", cumulative)
		if written != cumulative {
			t.Errorf("expected itemsWritten=%.0f after mixed batch, got %.0f", cumulative, written)
		}
	})
}

// TestH4_BatchSizeImmediate verifies that when exactly batchSize items are ingested
// the flush is triggered immediately (no tick needed): itemsInBuffer == 0 and
// itemsWritten == batchSize are visible in the very next status call.
func TestH4_BatchSizeImmediate(t *testing.T) {
	t.Cleanup(pipeline.Reset)

	// Read the batchSize we configure so the assertion stays in sync.
	const batchSize = 50

	outputDir := t.TempDir()
	cfgBytes, _ := json.Marshal(map[string]interface{}{
		"mode":            "file",
		"outputDir":       outputDir,
		"workerQueueSize": 100,
		"batchSize":       batchSize,
	})
	s := New()
	router := newTestRouter(s)

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfgBytes)); w.Code != http.StatusOK {
		t.Fatalf("configure: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	if w := do(t, router, http.MethodPost, "/gobbler/ingest", alphaJSON(t, batchSize)); w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}

	// The batch-size flush is synchronous inside Add(), so by the time the
	// ingest response is returned the items are already written. A single
	// status poll (with a brief yield for goroutine scheduling) is sufficient.
	written := waitForWritten(t, router, "alpha", batchSize)
	if written != float64(batchSize) {
		t.Errorf("expected itemsWritten=%d after batch-size flush, got %.0f", batchSize, written)
	}

	// itemsInBuffer must also be zero.
	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
	body := decodeJSON(t, w)
	writers, _ := body["writers"].(map[string]interface{})
	alphaEntry, _ := writers["alpha"].(map[string]interface{})
	if inBuf, _ := alphaEntry["itemsInBuffer"].(float64); inBuf != 0 {
		t.Errorf("expected itemsInBuffer=0 after batch-size flush, got %.0f", inBuf)
	}
}
