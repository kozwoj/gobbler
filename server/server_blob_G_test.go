package server

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// TestBlobG6_StopReconfigureRestart mirrors TestG6_StopReconfigureRestart for blob mode.
// It verifies that after stop → reconfigure (with a changed batchSize) → restart,
// definitions persist and blobs are correctly written to Azure in the second cycle.
// The reconfigure uses batchSize=5 so a small ingest immediately triggers a flush,
// confirming that the reconfigured settings are actually applied on restart.
// Skipped if ../tester/secrets.json is absent.
func TestBlobG6_StopReconfigureRestart(t *testing.T) {
	sec := loadBlobSecrets(t)
	t.Cleanup(pipeline.Reset)

	alphaContainer := newBlobContainer("alpha")
	t.Cleanup(func() { deleteContainer(sec, alphaContainer) })

	s := New()
	router := newTestRouter(s)

	// Cycle 1: configure (batchSize=50) → add alpha → start → stop (no ingest).
	configureBlobMode(t, router, sec) // default batchSize=50
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobAlphaDef(alphaContainer)); w.Code != http.StatusOK {
		t.Fatalf("add alpha (cycle 1): %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start (cycle 1): %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stop (cycle 1): %d %s", w.Code, w.Body.String())
	}

	// Verify no blobs in Azure from cycle 1 (no ingest happened).
	if count := countBlobsInContainer(t, sec, alphaContainer); count != 0 {
		t.Errorf("expected 0 blobs after cycle 1 (no ingest), got %d", count)
	}

	// Reconfigure with writerBatchSize=5 — small enough that 5 items trigger an immediate flush.
	configureBlobModeWithBatch(t, router, sec, 5)

	// Cycle 2: definitions persist across stop — no need to re-add alpha.
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start (cycle 2): %d %s", w.Code, w.Body.String())
	}

	// Ingest exactly 5 items — with batchSize=5 this triggers an immediate flush.
	g6Batch, err := tester.NewAlphaGenerator().GenerateJSONArray(5)
	if err != nil {
		t.Fatalf("generate alpha: %v", err)
	}
	w := do(t, router, http.MethodPost, "/gobbler/ingest", g6Batch)
	if w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["ingested"] != float64(5) {
		t.Errorf("expected ingested=5, got %v", body["ingested"])
	}

	// Wait for itemsWritten to reach 5 (confirms new batchSize is active).
	written := waitForWritten(t, router, "alpha", 5)
	if written != float64(5) {
		t.Errorf("expected alpha.itemsWritten=5 after batch-size flush, got %v", written)
	}

	do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// Blob must exist in Azure from cycle 2.
	count := countBlobsInContainer(t, sec, alphaContainer)
	if count == 0 {
		t.Errorf("expected blob in Azure container %q after cycle 2, found none", alphaContainer)
	}
}
