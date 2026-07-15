package writers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kozwoj/gobbler/items"
)

// buildDef parses a minimal item definition JSON for use in tests.
func buildDef(t *testing.T, raw string) items.ItemDefinition {
	t.Helper()
	var def items.ItemDefinition
	if err := items.CreateItemDefinition(raw, &def); err != nil {
		t.Fatalf("buildDef: %v", err)
	}
	return def
}

// writeJSON writes a {typeName}.json file into dir.
func writeSchemaJSON(t *testing.T, dir string, content []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "alpha.json"), content, 0644); err != nil {
		t.Fatalf("writeSchemaJSON: %v", err)
	}
}

// storedJSON builds the JSON that StoredItemDefinition would produce for the
// given columns (with ingest_time prepended), without calling the real function.
func storedJSON(cols []struct{ name, typ string }) []byte {
	type col struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	type schema struct {
		Name           string `json:"name"`
		OrderedColumns []col  `json:"orderedColumns"`
	}
	s := schema{Name: "alpha"}
	s.OrderedColumns = append(s.OrderedColumns, col{Name: "ingest_time", Type: "datetime"})
	for _, c := range cols {
		s.OrderedColumns = append(s.OrderedColumns, col{Name: c.name, Type: c.typ})
	}
	b, _ := json.Marshal(s)
	return b
}

const alphaDefRaw = `{
	"name": "alpha",
	"folder": "alpha",
	"latencyMinutes": 1,
	"orderedColumns": [
		{"name": "label", "type": "string"},
		{"name": "value", "type": "int"}
	]
}`

// ── checkSchemaConsistency unit tests ─────────────────────────────────────────

// SC1: No existing file → nil (first run).
func TestSC1_NoExistingFile(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw)
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err != nil {
		t.Errorf("expected nil for missing file, got: %v", err)
	}
}

// SC2: Existing file matches definition → nil.
func TestSC2_MatchingSchema(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw)
	writeSchemaJSON(t, dir, storedJSON([]struct{ name, typ string }{
		{"label", "string"},
		{"value", "int"},
	}))
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err != nil {
		t.Errorf("expected nil for matching schema, got: %v", err)
	}
}

// SC3: Column count mismatch → error.
func TestSC3_ColumnCountMismatch(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw) // 2 columns
	writeSchemaJSON(t, dir, storedJSON([]struct{ name, typ string }{
		{"label", "string"}, // only 1 column on disk
	}))
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err == nil {
		t.Error("expected error for column count mismatch, got nil")
	}
}

// SC4: Column name mismatch → error.
func TestSC4_ColumnNameMismatch(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw) // columns: label, value
	writeSchemaJSON(t, dir, storedJSON([]struct{ name, typ string }{
		{"label", "string"},
		{"count", "int"}, // "count" ≠ "value"
	}))
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err == nil {
		t.Error("expected error for column name mismatch, got nil")
	}
}

// SC5: Column type mismatch → error.
func TestSC5_ColumnTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw) // value is int
	writeSchemaJSON(t, dir, storedJSON([]struct{ name, typ string }{
		{"label", "string"},
		{"value", "real"}, // stored as real, definition says int
	}))
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err == nil {
		t.Error("expected error for column type mismatch, got nil")
	}
}

// SC6: Malformed JSON in existing file → error.
func TestSC6_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	def := buildDef(t, alphaDefRaw)
	if err := os.WriteFile(filepath.Join(dir, "alpha.json"), []byte("{bad json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkSchemaConsistency(filepath.Join(dir, "alpha.json"), def); err == nil {
		t.Error("expected error for malformed JSON, got nil")
	}
}

// ── NewFileWriter integration: schema conflict blocks writer creation ─────────

// SC7: NewFileWriter returns error when existing {typeName}.json conflicts.
func TestSC7_NewFileWriter_SchemaConflict(t *testing.T) {
	rootDir := t.TempDir()
	def := buildDef(t, alphaDefRaw)

	// Pre-create the alpha directory with a mismatched schema.
	alphaDir := filepath.Join(rootDir, "alpha")
	if err := os.MkdirAll(alphaDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeSchemaJSON(t, alphaDir, storedJSON([]struct{ name, typ string }{
		{"label", "string"},
		{"value", "real"}, // wrong type
	}))

	_, err := NewFileWriter(rootDir, def, 10)
	if err == nil {
		t.Error("expected error for schema conflict, got nil")
	}
}

// SC8: NewFileWriter succeeds when existing {typeName}.json matches.
func TestSC8_NewFileWriter_MatchingSchema(t *testing.T) {
	rootDir := t.TempDir()
	def := buildDef(t, alphaDefRaw)

	// First creation writes the file.
	if _, err := NewFileWriter(rootDir, def, 10); err != nil {
		t.Fatalf("first NewFileWriter: %v", err)
	}
	// Second creation (simulating restart) should succeed — schema matches.
	if _, err := NewFileWriter(rootDir, def, 10); err != nil {
		t.Errorf("second NewFileWriter (restart): %v", err)
	}
}
