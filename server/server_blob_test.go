package server

// Blob integration tests. Skipped automatically when ../tester/secrets.json
// is absent (no Azure credentials). The alphaJSON and waitForWritten helpers
// are defined in server_helpers_test.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

// ---- blob helpers ----

// blobSecrets holds the Azure credentials read from tester/secrets.json.
type blobSecrets struct {
	AccountName string `json:"accountName"`
	AccountKey  string `json:"accountKey"`
}

// loadBlobSecrets reads ../tester/secrets.json (relative to the server/ package
// directory where Go tests run). Calls t.Skip if the file is absent so blob
// integration tests are silently skipped in environments without Azure credentials.
func loadBlobSecrets(t *testing.T) blobSecrets {
	t.Helper()
	data, err := os.ReadFile("../tester/secrets.json")
	if err != nil {
		t.Skip("../tester/secrets.json not found — skipping blob integration test")
	}
	var s blobSecrets
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("could not parse tester/secrets.json: %v", err)
	}
	return s
}

// newBlobContainer generates a unique Azure-safe container name from a base word.
// Azure container names must be lowercase letters, digits, or hyphens; 3-63 chars.
// The hex timestamp suffix makes collisions between test runs extremely unlikely.
func newBlobContainer(base string) string {
	ts := fmt.Sprintf("%x", time.Now().UnixNano())
	return fmt.Sprintf("g-%s-%s", strings.ToLower(base), ts)
}

// blobAlphaDef returns an alpha item definition JSON using container as the folder.
func blobAlphaDef(container string) string {
	return fmt.Sprintf(
		`{"name":"alpha","documentation":"test blob alpha","folder":%q,"latencyMinutes":1,`+
			`"orderedColumns":[{"name":"alphaStr","type":"string"},{"name":"alphaInt","type":"int"},{"name":"alphaDate","type":"datetime"}]}`,
		container)
}

// blobBetaDef returns a beta item definition JSON using container as the folder.
func blobBetaDef(container string) string {
	return fmt.Sprintf(
		`{"name":"beta","documentation":"test blob beta","folder":%q,"latencyMinutes":2,`+
			`"orderedColumns":[{"name":"betaStr","type":"string"},{"name":"betaBool","type":"bool"},{"name":"betaReal","type":"real"}]}`,
		container)
}

// blobGammaDef returns a gamma item definition JSON using container as the folder.
func blobGammaDef(container string) string {
	return fmt.Sprintf(
		`{"name":"gamma","documentation":"test blob gamma","folder":%q,"latencyMinutes":3,`+
			`"orderedColumns":[{"name":"gammaInt","type":"int"},{"name":"gammaStr","type":"string"},{"name":"gammaDynamic","type":"dynamic"}]}`,
		container)
}

// configureBlobMode posts a valid blob-mode configure request using sec credentials.
func configureBlobMode(t *testing.T, router http.Handler, sec blobSecrets) {
	t.Helper()
	configureBlobModeWithBatch(t, router, sec, 50)
}

// configureBlobModeWithBatch is like configureBlobMode but with an explicit batchSize.
func configureBlobModeWithBatch(t *testing.T, router http.Handler, sec blobSecrets, writerBatchSize int) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"mode":            "blob",
		"accountName":     sec.AccountName,
		"accountKey":      sec.AccountKey,
		"writerQueueSize": 200,
		"writerBatchSize": writerBatchSize,
	})
	w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("blob configure failed: %d %s", w.Code, w.Body.String())
	}
}

// startWithAlphaBlobMode configures blob mode, adds the alpha definition using
// alphaContainer as the folder, and starts the pipeline.
// Registers pipeline.Reset as a cleanup and returns the router.
func startWithAlphaBlobMode(t *testing.T, sec blobSecrets, alphaContainer string) http.Handler {
	t.Helper()
	t.Cleanup(pipeline.Reset)
	s := New()
	router := newTestRouter(s)
	configureBlobMode(t, router, sec)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", blobAlphaDef(alphaContainer)); w.Code != http.StatusOK {
		t.Fatalf("add alpha blob def failed: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	return router
}

// countBlobsInContainer lists blobs in the named Azure container and returns the count.
// Returns 0 (without failing the test) if the container does not exist yet.
func countBlobsInContainer(t *testing.T, sec blobSecrets, container string) int {
	t.Helper()
	cred, err := azblob.NewSharedKeyCredential(sec.AccountName, sec.AccountKey)
	if err != nil {
		t.Errorf("countBlobsInContainer: credential: %v", err)
		return 0
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", sec.AccountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		t.Errorf("countBlobsInContainer: service client: %v", err)
		return 0
	}
	containerClient := client.ServiceClient().NewContainerClient(container)
	pager := containerClient.NewListBlobsFlatPager(nil)
	count := 0
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "ContainerNotFound") {
				return 0
			}
			t.Errorf("countBlobsInContainer: list: %v", err)
			return count
		}
		for _, blob := range page.Segment.BlobItems {
			// Skip schema files ({typeName}.json); count only data blobs.
			if !strings.HasSuffix(*blob.Name, ".json") {
				count++
			}
		}
	}
	return count
}

// waitForBlobCount polls the Azure container until it has at least minBlobs blobs,
// or the 10-second deadline is exceeded. Returns the final count observed.
func waitForBlobCount(t *testing.T, sec blobSecrets, container string, minBlobs int) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var count int
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		count = countBlobsInContainer(t, sec, container)
		if count >= minBlobs {
			return count
		}
	}
	return count
}

// deleteContainer removes the named Azure container and all its contents.
// Silently ignores errors (e.g. container not found) — safe to call from t.Cleanup.
func deleteContainer(sec blobSecrets, container string) {
	cred, err := azblob.NewSharedKeyCredential(sec.AccountName, sec.AccountKey)
	if err != nil {
		return
	}
	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", sec.AccountName)
	client, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return
	}
	containerClient := client.ServiceClient().NewContainerClient(container)
	_, _ = containerClient.Delete(context.Background(), nil)
}

// ---- BlobC: Happy path ----

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

// ---- BlobE: Hot-add and hot-remove while running ----

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

// ---- BlobF: Rotate ----

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

// ---- BlobG: Lifecycle edge cases ----

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

// ---- BlobH: Writer stats accuracy ----

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
