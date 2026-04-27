package server

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
)

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
func configureBlobModeWithBatch(t *testing.T, router http.Handler, sec blobSecrets, batchSize int) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"mode":            "blob",
		"accountName":     sec.AccountName,
		"accountKey":      sec.AccountKey,
		"workerQueueSize": 200,
		"batchSize":       batchSize,
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
		count += len(page.Segment.BlobItems)
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
