package server

import (
	"net/http"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
)

// startWithAlpha configures file mode, adds the alpha definition, and starts
// the pipeline. It registers pipeline.Reset as a cleanup and returns the
// router. Tests that call this must stop the pipeline themselves before the
// cleanup runs (or rely on the forced reset for goroutine hygiene).
func startWithAlpha(t *testing.T) http.Handler {
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
	return router
}

// ---- Category D: Ingest error handling ----

// D1: Item with an unknown type name lands in rejected; ingested count is 0.
func TestD1_UnknownType(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest",
		`[{"unknownType": {"someField": "value"}}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["ingested"] != float64(0) {
		t.Errorf("expected ingested=0, got %v", body["ingested"])
	}
	rejected, _ := body["rejected"].([]interface{})
	if len(rejected) != 1 {
		t.Fatalf("expected 1 rejected entry, got %d", len(rejected))
	}
	entry, _ := rejected[0].(map[string]interface{})
	if entry["typeName"] != "unknownType" {
		t.Errorf("expected typeName=unknownType in rejected entry, got %v", entry)
	}
	if _, hasErrors := entry["errors"]; !hasErrors {
		t.Errorf("expected errors slice in rejected entry, got %v", entry)
	}
}

// D2: Alpha item with a missing required field lands in rejected.
func TestD2_MissingRequiredField(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// alphaInt and alphaDate are missing — both are required with no default.
	w := do(t, router, http.MethodPost, "/gobbler/ingest",
		`[{"alpha": {"alphaStr": "only-string-provided"}}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["ingested"] != float64(0) {
		t.Errorf("expected ingested=0, got %v", body["ingested"])
	}
	rejected, _ := body["rejected"].([]interface{})
	if len(rejected) != 1 {
		t.Fatalf("expected 1 rejected entry, got %d", len(rejected))
	}
	entry, _ := rejected[0].(map[string]interface{})
	if entry["typeName"] != "alpha" {
		t.Errorf("expected typeName=alpha in rejected entry, got %v", entry)
	}
	errs, _ := entry["errors"].([]interface{})
	if len(errs) == 0 {
		t.Errorf("expected at least one error message in rejected entry, got none")
	}
}

// D3: Alpha item with a wrong field type (string instead of int) lands in rejected.
func TestD3_WrongFieldType(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// alphaInt must be a JSON number; supplying a string triggers ErrInvalidFieldType.
	w := do(t, router, http.MethodPost, "/gobbler/ingest",
		`[{"alpha": {"alphaStr": "hello", "alphaInt": "not-a-number", "alphaDate": "2026-04-25 10:00:00.000"}}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["ingested"] != float64(0) {
		t.Errorf("expected ingested=0, got %v", body["ingested"])
	}
	rejected, _ := body["rejected"].([]interface{})
	if len(rejected) != 1 {
		t.Fatalf("expected 1 rejected entry, got %d", len(rejected))
	}
	entry, _ := rejected[0].(map[string]interface{})
	if entry["typeName"] != "alpha" {
		t.Errorf("expected typeName=alpha in rejected entry, got %v", entry)
	}
	errs, _ := entry["errors"].([]interface{})
	if len(errs) == 0 {
		t.Errorf("expected at least one error message in rejected entry, got none")
	}
}

// D4: Mixed batch — ingested + rejected counts must equal total submitted.
func TestD4_MixedBatch(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// 2 valid alpha items + 1 unknown type + 1 alpha with wrong field type = 4 total
	const total = 4
	w := do(t, router, http.MethodPost, "/gobbler/ingest", `[
		{"alpha": {"alphaStr": "one",   "alphaInt": 1, "alphaDate": "2026-04-25 10:00:00.000"}},
		{"alpha": {"alphaStr": "two",   "alphaInt": 2, "alphaDate": "2026-04-25 10:00:01.000"}},
		{"unknownType": {"field": "value"}},
		{"alpha": {"alphaStr": "bad",   "alphaInt": "wrong", "alphaDate": "2026-04-25 10:00:02.000"}}
	]`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	ingested, _ := body["ingested"].(float64)
	rejected, _ := body["rejected"].([]interface{})

	if int(ingested)+len(rejected) != total {
		t.Errorf("expected ingested(%d) + rejected(%d) == %d, got %d + %d",
			int(ingested), len(rejected), total, int(ingested), len(rejected))
	}
	if int(ingested) != 2 {
		t.Errorf("expected ingested=2, got %d", int(ingested))
	}
	if len(rejected) != 2 {
		t.Errorf("expected 2 rejected entries, got %d", len(rejected))
	}
}

// D5: Malformed JSON body results in a parse error entry in rejected.
func TestD5_MalformedJSON(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest", `this is not json`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if body["ingested"] != float64(0) {
		t.Errorf("expected ingested=0, got %v", body["ingested"])
	}
	rejected, _ := body["rejected"].([]interface{})
	if len(rejected) == 0 {
		t.Fatalf("expected at least one rejected entry for malformed JSON, got none")
	}
	entry, _ := rejected[0].(map[string]interface{})
	if _, hasError := entry["error"]; !hasError {
		t.Errorf("expected error key in parse-error rejected entry, got %v", entry)
	}
}
