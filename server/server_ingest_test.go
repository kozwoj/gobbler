package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/tester"
)

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
	validAlpha, err := tester.NewAlphaGenerator().GenerateJSONArray(2)
	if err != nil {
		t.Fatalf("generate alpha: %v", err)
	}
	// Strip outer brackets from the valid portion and splice in the invalid items.
	batch := validAlpha[:len(validAlpha)-1] + `,
		{"unknownType": {"field": "value"}},
		{"alpha": {"alphaStr": "bad", "alphaInt": "wrong", "alphaDate": "2026-04-25 10:00:02.000"}}
	]`
	w := do(t, router, http.MethodPost, "/gobbler/ingest", batch)
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

// D5: Body that is not a JSON array at all returns 400.
func TestD5_NotJSONArray(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest", `this is not json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if _, hasError := body["error"]; !hasError {
		t.Errorf("expected error key in response, got %v", body)
	}
}

// D6: Empty JSON array returns 400.
func TestD6_EmptyArray(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/ingest", `[]`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if _, hasError := body["error"]; !hasError {
		t.Errorf("expected error key in response, got %v", body)
	}
}

// D7: Array where every item fails inner unmarshal (not a JSON object) returns 400.
func TestD7_AllItemsUnmarshalFail(t *testing.T) {
	router := startWithAlpha(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	// Each item's value is a string, not a JSON object — all fail inner unmarshal.
	w := do(t, router, http.MethodPost, "/gobbler/ingest",
		`[{"alpha": "not-an-object"}, {"alpha": "also-not-an-object"}]`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w)
	if _, hasError := body["error"]; !hasError {
		t.Errorf("expected error key in response, got %v", body)
	}
}

// ---- Category H: Writer stats accuracy ----

// TestH_StatsAccuracy verifies that status.writers["alpha"].itemsWritten
// always matches the cumulative ingested count reported by the ingest endpoint.
func TestH_StatsAccuracy(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)

	configureFileMode(t, router, outputDir)
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	var cumulative float64

	// H1 — ingest 10 items (below batchSize=50); wait for flush tick; itemsWritten == 10.
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

	// H3 — mixed batch: 5 valid alpha + 3 invalid (wrong type for alphaInt).
	// ingested must be 5, rejected must be 3, and itemsWritten increases by exactly 5.
	t.Run("H3_MixedBatch", func(t *testing.T) {
		const validCount = 5
		const invalidCount = 3
		valid := alphaJSON(t, validCount)
		// Build 3 items where alphaInt is a string (invalid type) — must stay hand-coded.
		invalidItems := make([]string, invalidCount)
		for i := range invalidItems {
			invalidItems[i] = fmt.Sprintf(
				`{"alpha":{"alphaStr":"bad%d","alphaInt":"notanint","alphaDate":"2026-04-25 10:00:00.000"}}`, i,
			)
		}
		invalid := strings.Join(invalidItems, ",")
		// Merge into one array: strip the trailing ']' from valid and prepend to invalid.
		batch := valid[:len(valid)-1] + "," + invalid + "]"

		w := do(t, router, http.MethodPost, "/gobbler/ingest", batch)
		if w.Code != http.StatusOK {
			t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
		}
		body := decodeJSON(t, w)
		if body["ingested"] != float64(validCount) {
			t.Errorf("expected ingested=%d, got %v", validCount, body["ingested"])
		}
		rejected, _ := body["rejected"].([]interface{})
		if len(rejected) != invalidCount {
			t.Errorf("expected %d rejected, got %d", invalidCount, len(rejected))
		}

		cumulative += validCount
		written := waitForWritten(t, router, "alpha", cumulative)
		if written != cumulative {
			t.Errorf("expected itemsWritten=%.0f after mixed batch, got %.0f", cumulative, written)
		}
	})
}

// TestH4_BatchSizeImmediate verifies that when exactly writerBatchSize items are ingested
// the flush is triggered immediately (no tick needed): itemsInBuffer == 0 and
// itemsWritten == writerBatchSize are visible in the very next status call.
func TestH4_BatchSizeImmediate(t *testing.T) {
	t.Cleanup(pipeline.Reset)

	// Read the writerBatchSize we configure so the assertion stays in sync.
	const writerBatchSize = 50

	outputDir := t.TempDir()
	cfgBytes, _ := json.Marshal(map[string]interface{}{
		"mode":            "file",
		"outputDir":       outputDir,
		"writerQueueSize": 100,
		"writerBatchSize": writerBatchSize,
	})
	s := New()
	router := newTestRouter(s)

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfgBytes)); w.Code != http.StatusOK {
		t.Fatalf("configure: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", alphaDef); w.Code != http.StatusOK {
		t.Fatalf("add alpha: %d %s", w.Code, w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	if w := do(t, router, http.MethodPost, "/gobbler/ingest", alphaJSON(t, writerBatchSize)); w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}

	// The writerBatchSize flush is synchronous inside Add(), so by the time the
	// ingest response is returned the items are already written. A single
	// status poll (with a brief yield for goroutine scheduling) is sufficient.
	written := waitForWritten(t, router, "alpha", writerBatchSize)
	if written != float64(writerBatchSize) {
		t.Errorf("expected itemsWritten=%d after batch-size flush, got %.0f", writerBatchSize, written)
	}

	// itemsInBuffer must also be zero.
	w := do(t, router, http.MethodGet, "/gobbler/pipeline/status", "")
	body := decodeJSON(t, w)
	writers, _ := body["writers"].(map[string]interface{})
	alphaEntry, _ := writers["alpha"].(map[string]interface{})
	if inBuf, _ := alphaEntry["itemsInBuffer"].(float64); inBuf != 0 {
		t.Errorf("expected itemsInBuffer=0 after batch-size flush, got %.0f", inBuf)
	}
}
