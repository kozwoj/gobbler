package query

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kozwoj/gobbler-query/api"
	"github.com/kozwoj/gobbler-query/query/batch"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func col(name string, t batch.ColumnType) batch.ColumnMeta {
	return batch.ColumnMeta{Name: name, Type: t}
}

// unmarshalRows decodes JSON produced by SerializeResult into a slice of
// map[string]any for easy assertion.
func unmarshalRows(t *testing.T, data []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return rows
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestSerializeResult_Empty(t *testing.T) {
	r := &api.Result{}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "[]" {
		t.Errorf("got %s, want []", data)
	}
}

func TestSerializeResult_SingleRow(t *testing.T) {
	r := &api.Result{
		Schema: []batch.ColumnMeta{
			col("userId", batch.TypeString),
			col("score", batch.TypeFloat64),
			col("active", batch.TypeBool),
			col("count", batch.TypeInt32),
		},
		Rows: [][]any{
			{"u123", float64(9.5), true, int32(42)},
		},
		Nulls: [][]bool{{false, false, false, false}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if row["userId"] != "u123" {
		t.Errorf("userId = %v, want u123", row["userId"])
	}
	if row["score"] != 9.5 {
		t.Errorf("score = %v, want 9.5", row["score"])
	}
	if row["active"] != true {
		t.Errorf("active = %v, want true", row["active"])
	}
	// JSON numbers unmarshal as float64; int32(42) → 42.0
	if row["count"] != float64(42) {
		t.Errorf("count = %v, want 42", row["count"])
	}
}

func TestSerializeResult_NullCell(t *testing.T) {
	r := &api.Result{
		Schema: []batch.ColumnMeta{
			col("region", batch.TypeString),
			col("score", batch.TypeFloat64),
		},
		Rows:  [][]any{{"eastus", nil}},
		Nulls: [][]bool{{false, true}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	if rows[0]["region"] != "eastus" {
		t.Errorf("region = %v, want eastus", rows[0]["region"])
	}
	if rows[0]["score"] != nil {
		t.Errorf("score = %v, want null", rows[0]["score"])
	}
}

func TestSerializeResult_DatetimeFormatted(t *testing.T) {
	ts := time.Date(2026, 5, 1, 0, 15, 33, 421_000_000, time.UTC)
	r := &api.Result{
		Schema: []batch.ColumnMeta{col("ingest_time", batch.TypeDatetime)},
		Rows:   [][]any{{ts}},
		Nulls:  [][]bool{{false}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	want := "2026-05-01 00:15:33.421"
	if rows[0]["ingest_time"] != want {
		t.Errorf("ingest_time = %q, want %q", rows[0]["ingest_time"], want)
	}
}

func TestSerializeResult_TimespanFormatted(t *testing.T) {
	dur := 90*time.Minute + 30*time.Second
	r := &api.Result{
		Schema: []batch.ColumnMeta{col("ttl", batch.TypeTimespan)},
		Rows:   [][]any{{dur}},
		Nulls:  [][]bool{{false}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	want := dur.String() // "1h30m30s"
	if rows[0]["ttl"] != want {
		t.Errorf("ttl = %q, want %q", rows[0]["ttl"], want)
	}
}

func TestSerializeResult_MultipleRows(t *testing.T) {
	r := &api.Result{
		Schema: []batch.ColumnMeta{
			col("requestId", batch.TypeString),
			col("statusCode", batch.TypeInt32),
		},
		Rows: [][]any{
			{"req-001", int32(200)},
			{"req-002", int32(401)},
			{"req-003", int32(500)},
		},
		Nulls: [][]bool{{false, false}, {false, false}, {false, false}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}
	if rows[1]["requestId"] != "req-002" {
		t.Errorf("row[1].requestId = %v, want req-002", rows[1]["requestId"])
	}
	if rows[2]["statusCode"] != float64(500) {
		t.Errorf("row[2].statusCode = %v, want 500", rows[2]["statusCode"])
	}
}

func TestSerializeResult_NullViaRowNil(t *testing.T) {
	// Nulls slice omitted — null detection falls back to Rows[i][j] == nil.
	r := &api.Result{
		Schema: []batch.ColumnMeta{
			col("a", batch.TypeString),
			col("b", batch.TypeString),
		},
		Rows: [][]any{{"hello", nil}},
	}
	data, err := SerializeResult(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rows := unmarshalRows(t, data)
	if rows[0]["a"] != "hello" {
		t.Errorf("a = %v, want hello", rows[0]["a"])
	}
	if rows[0]["b"] != nil {
		t.Errorf("b = %v, want null", rows[0]["b"])
	}
}
