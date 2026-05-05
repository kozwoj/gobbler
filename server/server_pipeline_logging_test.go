package server

// Tests for Phase 2 Step 4: gobbler-pipeline-event self-logging.
// Injects a spyClient (defined in server_ingest_logging_test.go) and verifies
// the event field emitted by the three lifecycle handlers.

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
)

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
