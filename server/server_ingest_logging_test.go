package server

// Tests for Phase 2 Step 2: gobbler-ingest-event self-logging.
// These tests inject a spyClient into s.logger and verify the fields
// emitted by logIngestEvent on the happy path and various 400 paths.

import (
	"net/http"
	"sync"
	"testing"

	gobblerclient "github.com/kozwoj/gobbler-client"
	"github.com/kozwoj/gobbler/pipeline"
)

// ---- spy client ----

type logCall struct {
	typeName string
	fields   map[string]any
}

type spyClient struct {
	mu   sync.Mutex
	logs []logCall
}

func (s *spyClient) Log(typeName string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, logCall{typeName: typeName, fields: fields})
	return nil
}
func (s *spyClient) Flush() error            { return nil }
func (s *spyClient) Close() error            { return nil }
func (s *spyClient) SwapServer(string) error { return nil }

func (s *spyClient) last() (logCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.logs) == 0 {
		return logCall{}, false
	}
	return s.logs[len(s.logs)-1], true
}

// startWithAlphaSpy starts the pipeline with the alpha definition, injects a
// spyClient into s.logger, and returns both the router and the spy.
func startWithAlphaSpy(t *testing.T) (http.Handler, *Server, *spyClient) {
	t.Helper()
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)
	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha failed: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	spy := &spyClient{}
	// Direct field injection is valid here because tests are in package server.
	s.logger = spy
	return router, s, spy
}

// ---- tests ----

// IL1: Happy path — all items accepted; event fields match.
func TestIL1_IngestLogging_HappyPath(t *testing.T) {
	router, _, spy := startWithAlphaSpy(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	body := `[{"alpha":{"alphaStr":"a","alphaInt":1,"alphaDate":"2024-01-01 00:00:00.000"}}]`
	w := do(t, router, http.MethodPost, "/gobbler/ingest", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call, got none")
	}
	if call.typeName != "gobbler-ingest-event" {
		t.Errorf("typeName = %q, want gobbler-ingest-event", call.typeName)
	}
	f := call.fields
	if f["itemsIn"] != 1 {
		t.Errorf("itemsIn = %v, want 1", f["itemsIn"])
	}
	if f["ingested"] != 1 {
		t.Errorf("ingested = %v, want 1", f["ingested"])
	}
	if f["rejected"] != 0 {
		t.Errorf("rejected = %v, want 0", f["rejected"])
	}
	if f["statusCode"] != http.StatusOK {
		t.Errorf("statusCode = %v, want 200", f["statusCode"])
	}
	if _, ok := f["durationMs"]; !ok {
		t.Error("durationMs missing from log fields")
	}
}

// IL2: Mixed batch — one item accepted, one rejected; itemsIn=2.
func TestIL2_IngestLogging_PartialRejection(t *testing.T) {
	router, _, spy := startWithAlphaSpy(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// First item valid alpha; second has unknown type.
	body := `[{"alpha":{"alphaStr":"x","alphaInt":2,"alphaDate":"2024-01-01 00:00:00.000"}},{"unknown":{"x":1}}]`
	w := do(t, router, http.MethodPost, "/gobbler/ingest", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call, got none")
	}
	f := call.fields
	if f["itemsIn"] != 2 {
		t.Errorf("itemsIn = %v, want 2", f["itemsIn"])
	}
	if f["ingested"] != 1 {
		t.Errorf("ingested = %v, want 1", f["ingested"])
	}
	if f["rejected"] != 1 {
		t.Errorf("rejected = %v, want 1", f["rejected"])
	}
	if f["statusCode"] != http.StatusOK {
		t.Errorf("statusCode = %v, want 200", f["statusCode"])
	}
}

// IL3: Invalid JSON array — 400 path; itemsIn=0.
func TestIL3_IngestLogging_InvalidJSONArray(t *testing.T) {
	router, _, spy := startWithAlphaSpy(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest", `not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call, got none")
	}
	f := call.fields
	if f["statusCode"] != http.StatusBadRequest {
		t.Errorf("statusCode = %v, want 400", f["statusCode"])
	}
	if f["ingested"] != 0 {
		t.Errorf("ingested = %v, want 0", f["ingested"])
	}
}

// IL4: Empty array — 400 path; itemsIn=0, rejected=0.
func TestIL4_IngestLogging_EmptyArray(t *testing.T) {
	router, _, spy := startWithAlphaSpy(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest", `[]`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call, got none")
	}
	f := call.fields
	if f["statusCode"] != http.StatusBadRequest {
		t.Errorf("statusCode = %v, want 400", f["statusCode"])
	}
	if f["ingested"] != 0 {
		t.Errorf("ingested = %v, want 0", f["ingested"])
	}
	if f["rejected"] != 0 {
		t.Errorf("rejected = %v, want 0", f["rejected"])
	}
}

// IL5: Nop logger (default) — no panic when logger is not configured.
func TestIL5_IngestLogging_NopLogger(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	// s.logger is Nop() by default — do NOT inject a spy.
	_ = s.logger.(gobblerclient.Client) // compile-time interface check

	router := newTestRouter(s)
	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha failed: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	body := `[{"alpha":{"alphaStr":"n","alphaInt":3,"alphaDate":"2024-01-01 00:00:00.000"}}]`
	w := do(t, router, http.MethodPost, "/gobbler/ingest", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// No assertion needed — just verifying no panic occurs.
}
