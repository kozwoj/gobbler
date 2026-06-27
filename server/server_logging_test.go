package server

// Self-logging tests. The spyClient type and startWithAlphaSpy helper are
// defined in server_helpers_test.go.

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
)

// ---- Ingest logging (gobbler-ingest-event) ----

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
	_ = s.logger

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

// ---- Pipeline logging (gobbler-pipeline-event) ----

// PL1: handlePipelineStart emits event="start" after the pipeline is up.
func TestPL1_PipelineLogging_Start(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)
	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha failed: %d %s", w.Code, w.Body.String())
	}

	spy := &spyClient{}
	s.logger = spy // inject before start so the start handler uses the spy

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start failed: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call from start, got none")
	}
	if call.typeName != "gobbler-pipeline-event" {
		t.Errorf("typeName = %q, want gobbler-pipeline-event", call.typeName)
	}
	if call.fields["event"] != "start" {
		t.Errorf("event = %v, want start", call.fields["event"])
	}
	if call.fields["instanceName"] != "test-instance" {
		t.Errorf("instanceName = %v, want test-instance", call.fields["instanceName"])
	}
}

// PL2: handlePipelineStop emits event="stop" before the logger is closed.
func TestPL2_PipelineLogging_Stop(t *testing.T) {
	router, s, spy := startWithAlphaSpy(t)
	// stop is called explicitly below; do NOT use defer here.

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stop failed: %d %s", w.Code, w.Body.String())
	}

	// The spy was replaced by Nop on stop, but we still hold the reference.
	_ = s // ensure s is referenced (the spy was injected into s.logger before stop)

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call from stop, got none")
	}
	if call.typeName != "gobbler-pipeline-event" {
		t.Errorf("typeName = %q, want gobbler-pipeline-event", call.typeName)
	}
	if call.fields["event"] != "stop" {
		t.Errorf("event = %v, want stop", call.fields["event"])
	}
	if call.fields["instanceName"] != "test-instance" {
		t.Errorf("instanceName = %v, want test-instance", call.fields["instanceName"])
	}
}

// PL3: handlePipelineRotate emits event="rotate" with the correct itemType.
func TestPL3_PipelineLogging_Rotate(t *testing.T) {
	router, _, spy := startWithAlphaSpy(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/pipeline/rotate", `{"typeName":"alpha"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d %s", w.Code, w.Body.String())
	}

	call, ok := spy.last()
	if !ok {
		t.Fatal("expected a log call from rotate, got none")
	}
	if call.typeName != "gobbler-pipeline-event" {
		t.Errorf("typeName = %q, want gobbler-pipeline-event", call.typeName)
	}
	if call.fields["event"] != "rotate" {
		t.Errorf("event = %v, want rotate", call.fields["event"])
	}
	if call.fields["itemType"] != "alpha" {
		t.Errorf("itemType = %v, want alpha", call.fields["itemType"])
	}
	if call.fields["instanceName"] != "test-instance" {
		t.Errorf("instanceName = %v, want test-instance", call.fields["instanceName"])
	}
}

// PL4: Nop logger (default) — no panic on start/stop/rotate.
func TestPL4_PipelineLogging_NopLogger(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/rotate", `{"typeName":"alpha"}`); w.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d %s", w.Code, w.Body.String())
	}
	// No assertion needed — verifying no panic.
}
