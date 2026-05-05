//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gobblerclient "github.com/kozwoj/gobbler-client"
	"github.com/kozwoj/gobbler/pipeline"
)

// startLiveServer creates a Server, registers defJSON, configures file mode with a
// temp dir, starts the pipeline, and wraps it in an httptest.Server.
// Cleanup stops the pipeline (via HTTP) and closes the httptest.Server.
func startLiveServer(t *testing.T, defJSON string) (*httptest.Server, http.Handler) {
	t.Helper()
	s := New()
	router := newTestRouter(s)

	w := do(t, router, http.MethodPost, "/gobbler/definition/add", defJSON)
	if w.Code != http.StatusOK {
		t.Fatalf("startLiveServer: definition/add %d %s", w.Code, w.Body.String())
	}

	configureFileMode(t, router, t.TempDir())

	w = do(t, router, http.MethodPost, "/gobbler/pipeline/start", "")
	if w.Code != http.StatusOK {
		t.Fatalf("startLiveServer: pipeline/start %d %s", w.Code, w.Body.String())
	}

	httpSrv := httptest.NewServer(router)
	t.Cleanup(func() {
		if s.IsRunning() {
			do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")
		}
		httpSrv.Close()
	})
	return httpSrv, router
}

// itemsInBuffer reads pipeline/status and returns the itemsInBuffer count for typeName.
func itemsInBuffer(t *testing.T, router http.Handler, typeName string) int {
	t.Helper()
	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
	if w.Code != http.StatusOK {
		t.Fatalf("pipeline/status: %d %s", w.Code, w.Body.String())
	}
	var m map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	writers, ok := m["writers"].(map[string]interface{})
	if !ok {
		return 0
	}
	typeStats, ok := writers[typeName].(map[string]interface{})
	if !ok {
		return 0
	}
	v, _ := typeStats["itemsInBuffer"].(float64)
	return int(v)
}

// waitForItems polls pipeline/status until itemsInBuffer >= want or timeout elapses.
func waitForItems(t *testing.T, router http.Handler, typeName string, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if itemsInBuffer(t, router, typeName) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got := itemsInBuffer(t, router, typeName)
	t.Fatalf("timed out: want %d items in buffer for %q, got %d", want, typeName, got)
}

// --- I9: Integration tests ---

// TestIntegration_I9_1_New_ValidatesRealServer confirms that gobblerclient.New succeeds
// when the target server is running and has the requested type registered.
func TestIntegration_I9_1_New_ValidatesRealServer(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	httpSrv, _ := startLiveServer(t, alphaDef)

	c, err := gobblerclient.New(
		httpSrv.URL,
		gobblerclient.WithTypes("alpha"),
		gobblerclient.WithFlushInterval(time.Hour), // suppress background flushes
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	_ = c.Close()
}

// TestIntegration_I9_2_LogFlush_ItemsReachServer logs items via the client, calls
// Flush, and verifies the items appear in the server's writer buffer (pipeline/status).
func TestIntegration_I9_2_LogFlush_ItemsReachServer(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	httpSrv, router := startLiveServer(t, alphaDef)

	c, err := gobblerclient.New(
		httpSrv.URL,
		gobblerclient.WithTypes("alpha"),
		gobblerclient.WithBatchSize(100),
		gobblerclient.WithFlushInterval(time.Hour),
	)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = c.Close() }()

	for i := 0; i < 3; i++ {
		if err := c.Log("alpha", map[string]any{
			"alphaStr":  "hello",
			"alphaInt":  i,
			"alphaDate": "2026-01-01 00:00:00.000",
		}); err != nil {
			t.Fatalf("Log(%d): %v", i, err)
		}
	}

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	waitForItems(t, router, "alpha", 3, time.Second)
}

// TestIntegration_I9_3_SwapServer_RoutesToNewServer creates a client pointing at srv1,
// logs and flushes to it, swaps to srv2, then confirms subsequent flushes reach srv2.
// The two servers are run sequentially (srv1 stopped before srv2 starts) to avoid
// conflicts in the shared pipeline routing table.
func TestIntegration_I9_3_SwapServer_RoutesToNewServer(t *testing.T) {
	t.Cleanup(pipeline.Reset)

	// --- Server 1 ---
	s1 := New()
	router1 := newTestRouter(s1)

	w := do(t, router1, http.MethodPost, "/gobbler/definition/add", alphaDef)
	if w.Code != http.StatusOK {
		t.Fatalf("srv1 definition/add: %d %s", w.Code, w.Body.String())
	}
	configureFileMode(t, router1, t.TempDir())
	w = do(t, router1, http.MethodPost, "/gobbler/pipeline/start", "")
	if w.Code != http.StatusOK {
		t.Fatalf("srv1 pipeline/start: %d %s", w.Code, w.Body.String())
	}
	httpSrv1 := httptest.NewServer(router1)
	t.Cleanup(httpSrv1.Close)

	// Create client pointing at srv1 with a long flush interval so no automatic
	// background flush fires during the test.
	c, err := gobblerclient.New(
		httpSrv1.URL,
		gobblerclient.WithTypes("alpha"),
		gobblerclient.WithBatchSize(100),
		gobblerclient.WithFlushInterval(time.Hour),
	)
	if err != nil {
		t.Fatalf("New() against srv1: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Log one item to srv1 and flush; verify it appears in srv1's buffer.
	if err := c.Log("alpha", map[string]any{
		"alphaStr":  "srv1-item",
		"alphaInt":  1,
		"alphaDate": "2026-01-01 00:00:00.000",
	}); err != nil {
		t.Fatalf("Log to srv1: %v", err)
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush() to srv1: %v", err)
	}
	waitForItems(t, router1, "alpha", 1, time.Second)

	// Stop srv1's pipeline; this calls pipeline.Reset() and removes "alpha" from
	// the routing table so srv2 can own it cleanly.
	w = do(t, router1, http.MethodPost, "/gobbler/pipeline/stop", "")
	if w.Code != http.StatusOK {
		t.Fatalf("srv1 pipeline/stop: %d %s", w.Code, w.Body.String())
	}

	// --- Server 2 ---
	s2 := New()
	router2 := newTestRouter(s2)

	w = do(t, router2, http.MethodPost, "/gobbler/definition/add", alphaDef)
	if w.Code != http.StatusOK {
		t.Fatalf("srv2 definition/add: %d %s", w.Code, w.Body.String())
	}
	configureFileMode(t, router2, t.TempDir())
	w = do(t, router2, http.MethodPost, "/gobbler/pipeline/start", "")
	if w.Code != http.StatusOK {
		t.Fatalf("srv2 pipeline/start: %d %s", w.Code, w.Body.String())
	}
	httpSrv2 := httptest.NewServer(router2)
	t.Cleanup(func() {
		if s2.IsRunning() {
			do(t, router2, http.MethodPost, "/gobbler/pipeline/stop", "")
		}
		httpSrv2.Close()
	})

	// Swap the client to srv2 (validates running + "alpha" present).
	if err := c.SwapServer(httpSrv2.URL); err != nil {
		t.Fatalf("SwapServer() to srv2: %v", err)
	}

	// Log one item and flush; it must reach srv2.
	if err := c.Log("alpha", map[string]any{
		"alphaStr":  "srv2-item",
		"alphaInt":  2,
		"alphaDate": "2026-01-01 00:00:00.000",
	}); err != nil {
		t.Fatalf("Log to srv2: %v", err)
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush() to srv2: %v", err)
	}
	waitForItems(t, router2, "alpha", 1, time.Second)
}
