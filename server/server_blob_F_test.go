package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// TestBlobF_Rotate mirrors TestF_Rotate for blob mode.
// F1: items are buffered before any flush (no blob in Azure yet).
// F2: rotate flushes the buffer and closes the blob.
// F3: ingesting more items after rotate creates a second blob in Azure.
// Skipped if ../tester/secrets.json is absent.
func TestBlobF_Rotate(t *testing.T) {
	sec := loadBlobSecrets(t)
	t.Cleanup(pipeline.Reset)

	alphaContainer := newBlobContainer("alpha")
	t.Cleanup(func() { deleteContainer(sec, alphaContainer) })

	router := startWithAlphaBlobMode(t, sec, alphaContainer)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// F1 — ingest 3 items (well below batchSize=50); itemsInBuffer > 0 and
	// currentOutput must be "" because no flush has occurred yet.
	t.Run("F1_BufferedBeforeFlush", func(t *testing.T) {
		f1Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(3)
		if err != nil {
			t.Fatalf("generate alpha: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", f1Batch)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}

		// Sleep briefly so the worker goroutine delivers items to the BlobWriter
		// buffer. 100 ms is well within the 500 ms tick interval.
		time.Sleep(100 * time.Millisecond)
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
		// No blob should exist in Azure yet.
		if count := countBlobsInContainer(t, sec, alphaContainer); count != 0 {
			t.Errorf("expected 0 blobs in Azure before flush, got %d", count)
		}
	})

	// F2 — rotate alpha; buffer must be flushed and blob closed.
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
		if written, _ := alphaEntry["itemsWritten"].(float64); written == 0 {
			t.Errorf("expected itemsWritten > 0 after rotate, got 0")
		}
		// Rotate closes the blob, so currentOutput must be empty.
		if cur, _ := alphaEntry["currentOutput"].(string); cur != "" {
			t.Errorf("expected currentOutput empty after rotate (blob closed), got %q", cur)
		}
		// Exactly 1 blob must now exist in Azure.
		count := waitForBlobCount(t, sec, alphaContainer, 1)
		if count != 1 {
			t.Errorf("expected 1 blob in Azure after rotate, got %d", count)
		}
	})

	// F3 — ingest more items and wait for the flush tick; a second blob must
	// appear in Azure (the first was written by the rotate in F2).
	t.Run("F3_SecondBlobAfterRotate", func(t *testing.T) {
		f3Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(2)
		if err != nil {
			t.Fatalf("generate alpha: %v", err)
		}
		w := do(t, router, http.MethodPost, "/gobbler/ingest", f3Batch)
		if w.Code != http.StatusOK {
			t.Fatalf("second ingest: %d %s", w.Code, w.Body.String())
		}

		// Wait up to 10 s for the flush tick to push the second batch to Azure.
		count := waitForBlobCount(t, sec, alphaContainer, 2)
		if count < 2 {
			t.Errorf("expected at least 2 blobs in Azure after rotate+re-ingest, got %d", count)
		}
	})
}
