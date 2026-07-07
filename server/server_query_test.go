package server

// Integration tests for the query endpoints:
//   GET  /gobbler/query/tables
//   POST /gobbler/query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kozwoj/gobbler/pipeline"
)

// ---- definition with an optional field (for null testing) ----

const queryDef = `{
	"name":           "querytest",
	"folder":         "querytest",
	"latencyMinutes": 1,
	"orderedColumns": [
		{"name": "label", "type": "string"},
		{"name": "value", "type": "int"},
		{"name": "note",  "type": "string", "optional": true}
	]
}`

// ---- helpers ----

// postQuery sends POST /gobbler/query with the given GQL string.
func postQuery(t *testing.T, router http.Handler, gql string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"query": gql})
	return do(t, router, http.MethodPost, "/gobbler/query", string(body))
}

// decodeQueryRows decodes a JSON-array query response into a slice of row maps.
func decodeQueryRows(t *testing.T, w *httptest.ResponseRecorder) []map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&rows); err != nil {
		t.Fatalf("decodeQueryRows: %v\nbody: %s", err, w.Body.String())
	}
	return rows
}

// startQueryTest configures file mode with batch size 1 (immediate flush),
// adds the querytest definition, starts the pipeline, and returns the router.
func startQueryTest(t *testing.T) http.Handler {
	t.Helper()
	t.Cleanup(pipeline.Reset)
	outputDir := t.TempDir()
	s := New()
	router := newTestRouter(s)

	cfgBytes, _ := json.Marshal(map[string]interface{}{
		"mode": "file", "outputDir": outputDir,
		"writerQueueSize": 200, "writerBatchSize": 1,
		"instanceName": "query-test",
	})
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/configure", string(cfgBytes)); w.Code != http.StatusOK {
		t.Fatalf("configure: %s", w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/definition/add", queryDef); w.Code != http.StatusOK {
		t.Fatalf("add queryDef: %s", w.Body.String())
	}
	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/start", ""); w.Code != http.StatusOK {
		t.Fatalf("start: %s", w.Body.String())
	}
	return router
}

// ingestQueryItems ingests three querytest items, one with a null optional field.
func ingestQueryItems(t *testing.T, router http.Handler) {
	t.Helper()
	payload := `[
		{"querytest": {"label": "alpha", "value": 10, "note": "first"}},
		{"querytest": {"label": "beta",  "value": 20}},
		{"querytest": {"label": "gamma", "value": 30, "note": "third"}}
	]`
	if w := do(t, router, http.MethodPost, "/gobbler/ingest", payload); w.Code != http.StatusOK {
		t.Fatalf("ingest: %d %s", w.Code, w.Body.String())
	}
	waitForWritten(t, router, "querytest", 3)
}

// ── QY1/QY2: preconditions ────────────────────────────────────────────────────

// QY1: POST /gobbler/query before configure → 409.
func TestQY1_Query_NotConfigured(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	router := newTestRouter(New())
	w := postQuery(t, router, "querytest(*)")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// QY2: GET /gobbler/query/tables before configure → 409.
func TestQY2_Tables_NotConfigured(t *testing.T) {
	t.Cleanup(pipeline.Reset)
	router := newTestRouter(New())
	w := do(t, router, http.MethodGet, "/gobbler/query/tables", "")
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// ── QY3/QY4: input validation ─────────────────────────────────────────────────

// QY3: Missing query field → 400.
func TestQY3_Query_MissingField(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodPost, "/gobbler/query", `{}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// QY4: Invalid GQL (parse error) → 400.
func TestQY4_Query_InvalidGQL(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := postQuery(t, router, "this is not valid GQL !!!")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ── QY5: happy path ───────────────────────────────────────────────────────────

// QY5: Ingest 3 items, query all → 200, 3 rows returned.
func TestQY5_Query_HappyPath(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	ingestQueryItems(t, router)

	w := postQuery(t, router, "querytest(*)")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := decodeQueryRows(t, w)
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
	// Every row must have ingest_time (prepended by Gobbler) plus the three user columns.
	for i, row := range rows {
		if _, ok := row["ingest_time"]; !ok {
			t.Errorf("row %d missing ingest_time", i)
		}
		if _, ok := row["label"]; !ok {
			t.Errorf("row %d missing label", i)
		}
	}
}

// ── QY6: where filter ─────────────────────────────────────────────────────────

// QY6: where filter returns only matching rows.
func TestQY6_Query_WhereFilter(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	ingestQueryItems(t, router)

	w := postQuery(t, router, `querytest(*) | where value >= 20`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := decodeQueryRows(t, w)
	if len(rows) != 2 {
		t.Errorf("expected 2 rows (value>=20), got %d", len(rows))
	}
}

// ── QY7: project ──────────────────────────────────────────────────────────────

// QY7: project limits columns to only those named.
func TestQY7_Query_Project(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	ingestQueryItems(t, router)

	w := postQuery(t, router, `querytest(*) | project label, value`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := decodeQueryRows(t, w)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// Only label and value should be present — no ingest_time, no note.
	row := rows[0]
	if _, ok := row["ingest_time"]; ok {
		t.Error("ingest_time should have been projected away")
	}
	if _, ok := row["note"]; ok {
		t.Error("note should have been projected away")
	}
	if _, ok := row["label"]; !ok {
		t.Error("label should be present after project")
	}
}

// ── QY8: null ─────────────────────────────────────────────────────────────────

// QY8: Optional field not supplied → JSON null in response.
func TestQY8_Query_NullField(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	ingestQueryItems(t, router)

	// Find the row with label=="beta" which has no note supplied.
	w := postQuery(t, router, `querytest(*) | where label == "beta"`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	rows := decodeQueryRows(t, w)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	noteVal, exists := rows[0]["note"]
	if !exists {
		t.Fatal("note key missing from row")
	}
	if noteVal != nil {
		t.Errorf("expected null note, got %v", noteVal)
	}
}

// ── QY9: empty result ─────────────────────────────────────────────────────────

// QY9: Query that matches no rows → [].
func TestQY9_Query_EmptyResult(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	ingestQueryItems(t, router)

	w := postQuery(t, router, `querytest(*) | where value > 9999`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "[]" && body != "[]\n" {
		t.Errorf("expected empty array, got %s", body)
	}
}

// ── QY10: query after stop ────────────────────────────────────────────────────

// QY10: Historical data is queryable after pipeline/stop.
func TestQY10_Query_AfterStop(t *testing.T) {
	router := startQueryTest(t)

	ingestQueryItems(t, router)

	if w := do(t, router, http.MethodPost, "/gobbler/pipeline/stop", ""); w.Code != http.StatusOK {
		t.Fatalf("stop: %s", w.Body.String())
	}

	w := postQuery(t, router, "querytest(*)")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after stop, got %d: %s", w.Code, w.Body.String())
	}
	rows := decodeQueryRows(t, w)
	if len(rows) != 3 {
		t.Errorf("expected 3 rows after stop, got %d", len(rows))
	}
}

// ── QY11: tables endpoint ─────────────────────────────────────────────────────

// QY11: GET /gobbler/query/tables returns the querytest entry after start.
func TestQY11_Tables_ReturnsEntries(t *testing.T) {
	router := startQueryTest(t)
	defer do(t, router, http.MethodPost, "/gobbler/pipeline/stop", "")

	w := do(t, router, http.MethodGet, "/gobbler/query/tables", "")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var entries []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&entries); err != nil {
		t.Fatalf("decode tables: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e["typeName"] == "querytest" {
			found = true
			if e["storageBucket"] != "querytest" {
				t.Errorf("storageBucket = %v, want querytest", e["storageBucket"])
			}
			if e["mode"] != "file" {
				t.Errorf("mode = %v, want file", e["mode"])
			}
		}
	}
	if !found {
		t.Errorf("querytest not found in tables response: %v", entries)
	}
}
