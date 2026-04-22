package items

import (
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
		"folder": "folderOne",
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
	if def.Folder != "folderOne" {
		t.Errorf("Folder: got %q, want %q", def.Folder, "folderOne")
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
	mustFail(t, `{"name": "timestamp", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrReservedName)
}

func TestError_InvalidName_StartsWithDigit(t *testing.T) {
	mustFail(t, `{"name": "1invalid", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFileName)
}

func TestError_InvalidName_ContainsSpace(t *testing.T) {
	mustFail(t, `{"name": "bad name", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrInvalidFileName)
}

/* ============================= folder field errors ============================= */

func TestError_FolderTooShort(t *testing.T) {
	mustFail(t, `{"name": "mytype", "folder": "ab", "orderedColumns": [{"name": "f1", "type": "string"}]}`, ErrFolderTooShort)
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
	json := `{"name": "mytype", "orderedColumns": [{"name": "timestamp", "type": "string"}]}`
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
