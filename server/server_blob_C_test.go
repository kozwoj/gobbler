package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// TestBlobC_HappyPath mirrors TestC_HappyPath for blob mode.
// It runs the full configure → add → start → ingest → flush → verify blobs → stop cycle.
// Skipped if ../tester/secrets.json is absent.
func TestBlobC_HappyPath(t *testing.T) {
	sec := loadBlobSecrets(t)
	t.Cleanup(pipeline.Reset)

	alphaContainer := newBlobContainer("alpha")
	betaContainer := newBlobContainer("beta")
	t.Cleanup(func() {
		deleteContainer(sec, alphaContainer)
		deleteContainer(sec, betaContainer)
	})

	s := New()
	router := newTestRouter(s)

	// C1 — configure (blob mode)
	t.Run("C1_Configure", func(t *testing.T) {
		configureBlobMode(t, router, sec)
	})

	// C2 — add alpha definition
	t.Run("C2_AddAlpha", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobAlphaDef(alphaContainer))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C3 — add beta definition
	t.Run("C3_AddBeta", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobBetaDef(betaContainer))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C4 — list definitions: must report 2
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

	// C5 — start pipeline; NewBlobWriter creates the Azure containers
	t.Run("C5_Start", func(t *testing.T) {
		w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	// C6 — status immediately after start: running=true, writers map with zeroed stats
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
				t.Fatalf("expected writers[%s] to be a map", typeName)
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

	// C8 — wait for flush tick (up to 10 s for Azure) then check itemsWritten in status
	t.Run("C8_StatsAfterFlush", func(t *testing.T) {
		deadline := time.Now().Add(10 * time.Second)
		var alphaWritten, betaWritten float64
		for time.Now().Before(deadline) {
			time.Sleep(200 * time.Millisecond)
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

	// C9 — blobs exist in both Azure containers
	t.Run("C9_BlobsInAzure", func(t *testing.T) {
		for _, container := range []string{alphaContainer, betaContainer} {
			count := countBlobsInContainer(t, sec, container)
			if count == 0 {
				t.Errorf("expected at least one blob in container %q, found none", container)
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
			t.Errorf("expected writers key to be absent after stop")
		}
	})
}
