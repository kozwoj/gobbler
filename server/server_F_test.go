package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
)

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
		w := do(t, router, http.MethodPost, "/gobbler/ingest", `[
			{"alpha": {"alphaStr": "one",   "alphaInt": 1, "alphaDate": "2026-04-25 10:00:00.000"}},
			{"alpha": {"alphaStr": "two",   "alphaInt": 2, "alphaDate": "2026-04-25 10:00:01.000"}},
			{"alpha": {"alphaStr": "three", "alphaInt": 3, "alphaDate": "2026-04-25 10:00:02.000"}}
		]`)
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
	// appear in alphaFolder (the first was written by the rotate in F2).
	t.Run("F3_SecondFileAfterRotate", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/ingest", `[
			{"alpha": {"alphaStr": "four", "alphaInt": 4, "alphaDate": "2026-04-25 10:00:03.000"}},
			{"alpha": {"alphaStr": "five", "alphaInt": 5, "alphaDate": "2026-04-25 10:00:04.000"}}
		]`)
		if w.Code != http.StatusOK {
			t.Fatalf("second ingest: %d %s", w.Code, w.Body.String())
		}

		// Wait up to 3 s for the tick to flush the second batch.
		alphaDir := filepath.Join(outputDir, "alphaFolder")
		deadline := time.Now().Add(3 * time.Second)
		var fileCount int
		for time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
			entries, err := os.ReadDir(alphaDir)
			if err != nil {
				t.Fatalf("could not read alphaFolder: %v", err)
			}
			fileCount = len(entries)
			if fileCount >= 2 {
				break
			}
		}
		if fileCount < 2 {
			t.Errorf("expected at least 2 CSV files in alphaFolder after rotate+re-ingest, got %d", fileCount)
		}
	})
}
