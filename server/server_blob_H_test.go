package server

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/tester"
)

// TestBlobH1_StatsAccuracy mirrors TestH_StatsAccuracy/H1_TenItems for blob mode.
// It verifies that the BlobWriter's itemsWritten counter stays in sync with the
// number of items accepted by the ingest endpoint across multiple ingest calls.
// Skipped if ../tester/secrets.json is absent.
func TestBlobH1_StatsAccuracy(t *testing.T) {
	sec := loadBlobSecrets(t)

	alphaContainer := newBlobContainer("alpha")
	t.Cleanup(func() { deleteContainer(sec, alphaContainer) })

	router := startWithAlphaBlobMode(t, sec, alphaContainer)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	var cumulative float64

	// H1 — ingest 10 items (below writerBatchSize=50); wait for flush; itemsWritten == 10.
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

	// H2 — ingest another 5 items; cumulative itemsWritten must reach 15.
	t.Run("H2_FiveMore", func(t *testing.T) {
		const n = 5
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
			t.Errorf("expected itemsWritten=%.0f after second batch, got %.0f", cumulative, written)
		}
	})

	// H3 — blob must exist in Azure with all 15 items flushed.
	t.Run("H3_BlobExistsInAzure", func(t *testing.T) {
		count := countBlobsInContainer(t, sec, alphaContainer)
		if count == 0 {
			t.Errorf("expected at least one blob in Azure container %q, found none", alphaContainer)
		}
	})
}

// TestBlobH4_BatchSizeImmediate verifies that when exactly writerBatchSize items are
// ingested the BlobWriter flushes synchronously: itemsInBuffer == 0 and
// itemsWritten == writerBatchSize are visible in the very next status call.
// Skipped if ../tester/secrets.json is absent.
func TestBlobH4_BatchSizeImmediate(t *testing.T) {
	sec := loadBlobSecrets(t)

	const writerBatchSize = 10 // small enough for a quick test, large enough to confirm threshold logic

	alphaContainer := newBlobContainer("alpha")
	t.Cleanup(func() { deleteContainer(sec, alphaContainer) })

	s := New()
	router := newTestRouter(s)

	configureBlobModeWithBatch(t, router, sec, writerBatchSize)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobAlphaDef(alphaContainer)); w.Code != http.StatusOK {
		t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	batch, err := tester.NewAlphaGenerator().GenerateJSONArray(writerBatchSize)
	if err != nil {
		t.Fatalf("generate alpha: %v", err)
	}
	if w := do(t, router, http.MethodPost, "/gobbler/ingest", batch); w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}

	// The batch-size flush is triggered synchronously inside BlobWriter.Add,
	// so itemsWritten should reach batchSize quickly.
	written := waitForWritten(t, router, "alpha", writerBatchSize)
	if written != float64(writerBatchSize) {
		t.Errorf("expected itemsWritten=%d after batch-size flush, got %.0f", writerBatchSize, written)
	}

	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
	body := decodeJSON(t, w)
	writers, _ := body["writers"].(map[string]interface{})
	alphaEntry, _ := writers["alpha"].(map[string]interface{})
	if inBuf, _ := alphaEntry["itemsInBuffer"].(float64); inBuf != 0 {
		t.Errorf("expected itemsInBuffer=0 after batch-size flush, got %.0f", inBuf)
	}
}
