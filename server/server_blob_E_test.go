package server

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// TestBlobE_HotAddRemove mirrors TestE_HotAddRemove for blob mode.
// E1–E3: start with alpha only, hot-add gamma, ingest gamma items and wait for flush.
// E4: hot-remove gamma — verifies the blob was flushed to Azure on shutdown.
// E5 (ingest after remove is rejected) is mode-independent and covered by the file tests.
// Skipped if ../tester/secrets.json is absent.
func TestBlobE_HotAddRemove(t *testing.T) {
	sec := loadBlobSecrets(t)
	t.Cleanup(pipeline.Reset)

	alphaContainer := newBlobContainer("alpha")
	gammaContainer := newBlobContainer("gamma")
	t.Cleanup(func() {
		deleteContainer(sec, alphaContainer)
		deleteContainer(sec, gammaContainer)
	})

	s := New()
	router := newTestRouter(s)

	// E1 — start with only alpha; writers map must have exactly alpha.
	t.Run("E1_StartAlphaOnly", func(t *testing.T) {
		configureBlobMode(t, router, sec)
		if w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobAlphaDef(alphaContainer)); w.Code != http.StatusOK {
			t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
		}
		if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
			t.Fatalf("start: %d %s", w.Code, w.Body.String())
		}

		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, ok := body["writers"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected writers map in status")
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
		if w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobGammaDef(gammaContainer)); w.Code != http.StatusOK {
			t.Fatalf("hot-add gamma: %d %s", w.Code, w.Body.String())
		}

		w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
		body := decodeJSON(t, w)
		writers, ok := body["writers"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected writers map after hot-add")
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

		// waitForWritten polls the status endpoint (defined in server_E_test.go).
		written := waitForWritten(t, router, "gamma", gammaCount)
		if written != float64(gammaCount) {
			t.Errorf("expected gamma.itemsWritten=%d after flush, got %v", gammaCount, written)
		}
	})

	// E4 — hot-remove gamma; it must disappear from status writers and its blob must
	// be flushed to Azure (cancel+Wait triggers the shutdown flush in BlobWriter).
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

		// gamma's blob must exist in Azure (flushed by BlobWriter shutdown).
		count := waitForBlobCount(t, sec, gammaContainer, 1)
		if count == 0 {
			t.Errorf("expected gamma blob in Azure container %q after hot-remove, found none", gammaContainer)
		}
	})

	// cleanup — stop the pipeline so remaining writers (alpha) are closed.
	do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")
}
