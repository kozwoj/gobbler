package items

import (
	"encoding/json"
	"errors"
	"testing"
)

/* ============================= helpers ============================= */

func mustCreate(t *testing.T, json string) ItemDefinition {
	t.Helper()
	var def ItemDefinition
	if err := CreateItemDefinition(json, &def); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return def
}

func mustFail(t *testing.T, json string, wantErr error) {
	t.Helper()
	var def ItemDefinition
	err := CreateItemDefinition(json, &def)
	if err == nil {
		t.Fatalf("expected error %v, got nil", wantErr)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
}

/* ============================= valid definitions ============================= */

func TestValidDefinition_AllScalars(t *testing.T) {
	json := `{
		"name": "allscalars",
		"documentation": "table with all scalar types",
		"folder": "folder-one",
		"latencyMinutes": 10,
		"orderedColumns": [
			{"name": "_string",   "type": "string",   "defaultValue": "value", "optional": true},
			{"name": "_boolean",  "type": "bool",     "optional": false},
			{"name": "_datetime", "type": "datetime", "defaultValue": "2000-01-01 00:00:01.100", "optional": false},
			{"name": "_dynamic",  "type": "dynamic",  "defaultValue": "{\"key\": \"value\"}", "optional": false},
			{"name": "_int",      "type": "int",      "defaultValue": 10, "optional": false},
			{"name": "_real",     "type": "real",     "defaultValue": 55.55},
			{"name": "_timespan", "type": "timespan", "optional": true}
		]
	}`
	def := mustCreate(t, json)

	if def.TypeName != "allscalars" {
		t.Errorf("TypeName: got %q, want %q", def.TypeName, "allscalars")
	}
	if def.Folder != "folder-one" {
		t.Errorf("Folder: got %q, want %q", def.Folder, "folder-one")
	}
	if def.Latency != 10 {
		t.Errorf("Latency: got %d, want 10", def.Latency)
	}
	if len(def.Columns) != 7 {
		t.Fatalf("Columns: got %d, want 7", len(def.Columns))
	}
	// spot-check column types and defaults
	if def.Columns[0].ValueType != ColumnTypeString || def.Columns[0].DefaultValue != "value" || !def.Columns[0].Optional {
		t.Errorf("_string column unexpected: %+v", def.Columns[0])
	}
	if def.Columns[4].ValueType != ColumnTypeInt || def.Columns[4].DefaultValue != 10 {
		t.Errorf("_int column unexpected: %+v", def.Columns[4])
	}
	if def.Columns[5].ValueType != ColumnTypeReal || def.Columns[5].DefaultValue != 55.55 {
		t.Errorf("_real column unexpected: %+v", def.Columns[5])
	}
}

func TestValidDefinition_FolderDefaultsToName(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "string"}]}`
	def := mustCreate(t, json)
	if def.Folder != "mytype" {
		t.Errorf("Folder should default to name, got %q", def.Folder)
	}
}

func TestValidDefinition_LatencyDefaultsToOne(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "string"}]}`
	def := mustCreate(t, json)
	if def.Latency != 1 {
		t.Errorf("Latency should default to 1, got %d", def.Latency)
	}
}

func TestValidDefinition_DocumentationOptional(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "string"}]}`
	def := mustCreate(t, json)
	if def.Documentation != "" {
		t.Errorf("Documentation should be empty, got %q", def.Documentation)
	}
}

func TestValidDefinition_OptionalDefaultsToFalse(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "string"}]}`
	def := mustCreate(t, json)
	if def.Columns[0].Optional {
		t.Errorf("optional should default to false")
	}
}

func TestValidDefinition_IntDefaultValue(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "count", "type": "int", "defaultValue": 42}]}`
	def := mustCreate(t, json)
	if def.Columns[0].DefaultValue != 42 {
		t.Errorf("int default: got %v, want 42", def.Columns[0].DefaultValue)
	}
}

func TestValidDefinition_DatetimeWithoutMilliseconds(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "ts", "type": "datetime", "defaultValue": "2024-06-01 12:00:00"}]}`
	mustCreate(t, json)
}

func TestValidDefinition_TimespanDefault(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "dur", "type": "timespan", "defaultValue": "1h30m"}]}`
	def := mustCreate(t, json)
	if def.Columns[0].DefaultValue != "1h30m" {
		t.Errorf("timespan default: got %v, want 1h30m", def.Columns[0].DefaultValue)
	}
}

/* ============================= name field errors ============================= */

func TestError_MissingName(t *testing.T) {
	mustFail(t, `{"orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrMissingNameField)
}

func TestError_ReservedName_Timestamp(t *testing.T) {
	mustFail(t, `{"name": "ingest_time", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrReservedName)
}

func TestError_InvalidName_Uppercase(t *testing.T) {
	mustFail(t, `{"name": "Invalid", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFileName)
}

func TestError_InvalidName_ContainsSpace(t *testing.T) {
	mustFail(t, `{"name": "bad name", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFileName)
}

/* ============================= folder field errors ============================= */

func TestError_FolderTooShort(t *testing.T) {
	mustFail(t, `{"name": "mytype", "folder": "ab", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFolderField)
}

func TestError_FolderInvalid(t *testing.T) {
	mustFail(t, `{"name": "mytype", "folder": "bad/folder", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFolderField)
}

/* ============================= latency errors ============================= */

func TestError_NegativeLatency(t *testing.T) {
	mustFail(t, `{"name": "mytype", "latencyMinutes": -1, "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrNegativeLatency)
}

/* ============================= columns errors ============================= */

func TestError_MissingColumns(t *testing.T) {
	mustFail(t, `{"name": "mytype"}`, ErrMissingColumns)
}

func TestError_EmptyColumns(t *testing.T) {
	mustFail(t, `{"name": "mytype", "orderedColumns": []}`, ErrEmptyColumns)
}

func TestError_DuplicateColumnName(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [
		{"name": "f1", "type": "string"},
		{"name": "f1", "type": "int"}
	]}`
	mustFail(t, json, ErrDuplicateColumnName)
}

func TestError_ReservedColumnName_Timestamp(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "ingest_time", "type": "string"}]}`
	mustFail(t, json, ErrReservedName)
}

func TestError_UnsupportedColumnType(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "blob"}]}`
	mustFail(t, json, ErrUnsupportedColumnType)
}

func TestError_MissingColumnType(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1"}]}`
	mustFail(t, json, ErrMissingColumnType)
}

/* ============================= defaultValue type mismatch errors ============================= */

func TestError_DefaultValue_BoolMismatch(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "bool", "defaultValue": "notabool"}]}`
	mustFail(t, json, ErrInconsistentDefaultValue)
}

/* ============================= fixtures shared with conversion_test.go ============================= */

const alphaJSON = `{
	"name": "alpha",
	"documentation": "test definition alpha with string, int and datetime types",
	"folder": "alpha-folder",
	"latencyMinutes": 1,
	"orderedColumns": [
		{"name": "alphaStr",  "type": "string"},
		{"name": "alphaInt",  "type": "int"},
		{"name": "alphaDate", "type": "datetime"}
	]
}`

const betaJSON = `{
	"name": "beta",
	"documentation": "test definition beta with string, bool and real types",
	"folder": "beta-folder",
	"latencyMinutes": 2,
	"orderedColumns": [
		{"name": "betaStr",  "type": "string"},
		{"name": "betaBool", "type": "bool"},
		{"name": "betaReal", "type": "real"}
	]
}`

const gammaJSON = `{
	"name": "gamma",
	"documentation": "test definition gamma with int, string, and dynamic types",
	"folder": "gamma-folder",
	"latencyMinutes": 3,
	"orderedColumns": [
		{"name": "gammaInt",     "type": "int"},
		{"name": "gammaStr",     "type": "string"},
		{"name": "gammaDynamic", "type": "dynamic"}
	]
}`

// buildDefinitionList parses alpha, beta, gamma and returns a populated DefinitionList.
func buildDefinitionList(t *testing.T) DefinitionList {
	t.Helper()
	dl := make(DefinitionList)
	for _, raw := range []string{alphaJSON, betaJSON, gammaJSON} {
		var def ItemDefinition
		if err := CreateItemDefinition(raw, &def); err != nil {
			t.Fatalf("CreateItemDefinition failed: %v", err)
		}
		if err := dl.AddDefinition(def); err != nil {
			t.Fatalf("AddDefinition failed: %v", err)
		}
	}
	return dl
}

/* ============================= DefinitionList tests ============================= */

func TestDefinitionList_AddAndGet(t *testing.T) {
	dl := buildDefinitionList(t)

	def, err := dl.GetDefinition("alpha")
	if err != nil {
		t.Fatalf("GetDefinition(alpha): %v", err)
	}
	if def.TypeName != "alpha" {
		t.Errorf("TypeName: got %q, want %q", def.TypeName, "alpha")
	}
	if def.Folder != "alpha-folder" {
		t.Errorf("Folder: got %q, want %q", def.Folder, "alpha-folder")
	}
	if def.Latency != 1 {
		t.Errorf("Latency: got %d, want 1", def.Latency)
	}
	if len(def.Columns) != 3 {
		t.Errorf("Columns: got %d, want 3", len(def.Columns))
	}
}

func TestDefinitionList_AllThreeDefinitions(t *testing.T) {
	dl := buildDefinitionList(t)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if _, err := dl.GetDefinition(name); err != nil {
			t.Errorf("GetDefinition(%q): %v", name, err)
		}
	}
}

func TestDefinitionList_AddDuplicate(t *testing.T) {
	dl := buildDefinitionList(t)
	var def ItemDefinition
	if err := CreateItemDefinition(alphaJSON, &def); err != nil {
		t.Fatalf("CreateItemDefinition: %v", err)
	}
	err := dl.AddDefinition(def)
	if err == nil {
		t.Fatal("expected error adding duplicate, got nil")
	}
	if !errors.Is(err, ErrDefinitionAlreadyExists) {
		t.Errorf("expected ErrDefinitionAlreadyExists, got %v", err)
	}
}

func TestDefinitionList_GetNotFound(t *testing.T) {
	dl := buildDefinitionList(t)
	_, err := dl.GetDefinition("noSuchType")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDefinitionNotFound) {
		t.Errorf("expected ErrDefinitionNotFound, got %v", err)
	}
}

func TestError_DefaultValue_IntFractional(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "int", "defaultValue": 3.14}]}`
	mustFail(t, json, ErrInconsistentDefaultValue)
}

func TestError_DefaultValue_DatetimeInvalid(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "datetime", "defaultValue": "not-a-date"}]}`
	mustFail(t, json, ErrInconsistentDefaultValue)
}

func TestError_DefaultValue_DynamicInvalidJSON(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "dynamic", "defaultValue": "{bad json}"}]}`
	mustFail(t, json, ErrInconsistentDefaultValue)
}

func TestError_DefaultValue_TimespanInvalid(t *testing.T) {
	json := `{"name": "mytype", "orderedColumns": [{"name": "f1", "type": "timespan", "defaultValue": "2days"}]}`
	mustFail(t, json, ErrInconsistentDefaultValue)
}

/* ============================= StoredItemDefinition ============================= */

func TestStoredItemDefinition_TimestampPrepended(t *testing.T) {
	def := mustCreate(t, `{"name": "evt", "folder": "evt-folder", "orderedColumns": [
		{"name": "vmId", "type": "string"},
		{"name": "duration", "type": "timespan"}
	]}`)
	data, err := StoredItemDefinition(def)
	if err != nil {
		t.Fatalf("StoredItemDefinition: %v", err)
	}
	// Unmarshal and inspect
	var got storedItemDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Name != "evt" {
		t.Errorf("name: got %q, want %q", got.Name, "evt")
	}
	if len(got.OrderedColumns) != 3 {
		t.Fatalf("column count: got %d, want 3", len(got.OrderedColumns))
	}
	first := got.OrderedColumns[0]
	if first.Name != "ingest_time" || first.Type != "datetime" {
		t.Errorf("first column: got {%q %q}, want {ingest_time datetime}", first.Name, first.Type)
	}
}

func TestStoredItemDefinition_ColumnOrder(t *testing.T) {
	def := mustCreate(t, `{"name": "multi", "folder": "multi-folder", "orderedColumns": [
		{"name": "a", "type": "int"},
		{"name": "b", "type": "bool"},
		{"name": "c", "type": "dynamic"},
		{"name": "d", "type": "real"},
		{"name": "e", "type": "datetime"}
	]}`)
	data, err := StoredItemDefinition(def)
	if err != nil {
		t.Fatalf("StoredItemDefinition: %v", err)
	}
	var got storedItemDef
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []storedColumn{
		{Name: "ingest_time", Type: "datetime"},
		{Name: "a", Type: "int"},
		{Name: "b", Type: "bool"},
		{Name: "c", Type: "dynamic"},
		{Name: "d", Type: "real"},
		{Name: "e", Type: "datetime"},
	}
	if len(got.OrderedColumns) != len(want) {
		t.Fatalf("column count: got %d, want %d", len(got.OrderedColumns), len(want))
	}
	for i, w := range want {
		g := got.OrderedColumns[i]
		if g != w {
			t.Errorf("column[%d]: got {%q %q}, want {%q %q}", i, g.Name, g.Type, w.Name, w.Type)
		}
	}
}
