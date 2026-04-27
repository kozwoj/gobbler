package items

import (
	"errors"
	"strings"
	"testing"
)

/* ============================= test fixtures ============================= */

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

/* ============================= SplitInput tests ============================= */

func TestSplitInput_ValidMixed(t *testing.T) {
	input := []byte(`[
		{"alpha": {"alphaStr": "hello", "alphaInt": 1, "alphaDate": "2024-01-01 00:00:00"}},
		{"beta":  {"betaStr": "world", "betaBool": true, "betaReal": 3.14}},
		{"gamma": {"gammaInt": 42, "gammaStr": "foo", "gammaDynamic": "{\"k\":\"v\"}"}}
	]`)
	items, errs := SplitInput(input)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].ItemTypeName != "alpha" {
		t.Errorf("item[0] type: got %q, want %q", items[0].ItemTypeName, "alpha")
	}
	if items[1].ItemTypeName != "beta" {
		t.Errorf("item[1] type: got %q, want %q", items[1].ItemTypeName, "beta")
	}
	if items[2].ItemTypeName != "gamma" {
		t.Errorf("item[2] type: got %q, want %q", items[2].ItemTypeName, "gamma")
	}
}

func TestSplitInput_NotJSONArray(t *testing.T) {
	_, errs := SplitInput([]byte(`{"not": "an array"}`))
	if len(errs) == 0 {
		t.Fatal("expected error, got none")
	}
	if !errors.Is(errs[0], ErrInvalidJSONArray) {
		t.Errorf("expected ErrInvalidJSONArray, got %v", errs[0])
	}
}

func TestSplitInput_ItemWithMultipleKeys(t *testing.T) {
	input := []byte(`[{"alpha": {}, "beta": {}}]`)
	items, errs := SplitInput(input)
	if len(errs) == 0 {
		t.Fatal("expected error for multi-key item, got none")
	}
	if !errors.Is(errs[0], ErrInvalidItemStructure) {
		t.Errorf("expected ErrInvalidItemStructure, got %v", errs[0])
	}
	if len(items) != 0 {
		t.Errorf("expected 0 valid items, got %d", len(items))
	}
}

func TestSplitInput_PartialFailure(t *testing.T) {
	// second item has bad inner JSON
	input := []byte(`[
		{"alpha": {"alphaStr": "ok"}},
		{"beta": "not-an-object"},
		{"gamma": {"gammaInt": 1}}
	]`)
	items, errs := SplitInput(input)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errs))
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 valid items, got %d", len(items))
	}
}

func TestSplitInput_EmptyArray(t *testing.T) {
	items, errs := SplitInput([]byte(`[]`))
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}
}

/* ============================= ConvertItem tests ============================= */

const fixedTimestamp = "2024-06-01 10:00:00.000"

func TestConvertItem_Alpha(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "alpha",
		ItemData: map[string]interface{}{
			"alphaStr":  "hello",
			"alphaInt":  float64(42),
			"alphaDate": "2024-01-15 08:30:00.000",
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	expected := fixedTimestamp + ",hello,42,2024-01-15 08:30:00.000"
	if csv != expected {
		t.Errorf("csv:\n  got  %q\n  want %q", csv, expected)
	}
}

func TestConvertItem_Beta(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "beta",
		ItemData: map[string]interface{}{
			"betaStr":  "world",
			"betaBool": true,
			"betaReal": 3.14,
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	expected := fixedTimestamp + ",world,true,3.14"
	if csv != expected {
		t.Errorf("csv:\n  got  %q\n  want %q", csv, expected)
	}
}

func TestConvertItem_Gamma_DynamicEscaped(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "gamma",
		ItemData: map[string]interface{}{
			"gammaInt":     float64(7),
			"gammaStr":     "test",
			"gammaDynamic": `{"key":"value"}`,
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	// dynamic field should be wrapped in quotes with internal quotes doubled
	if !strings.Contains(csv, `"{`) {
		t.Errorf("dynamic field not CSV-escaped in: %q", csv)
	}
}

func TestConvertItem_StringWithCommaIsEscaped(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "alpha",
		ItemData: map[string]interface{}{
			"alphaStr":  "hello, world",
			"alphaInt":  float64(1),
			"alphaDate": "2024-01-01 00:00:00",
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if !strings.Contains(csv, `"hello, world"`) {
		t.Errorf("string with comma not quoted in: %q", csv)
	}
}

func TestConvertItem_UnknownType(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "noSuchType",
		ItemData:     map[string]interface{}{},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) == 0 {
		t.Fatal("expected error for unknown type, got none")
	}
	if !errors.Is(errs[0], ErrItemTypeNotDefined) {
		t.Errorf("expected ErrItemTypeNotDefined, got %v", errs[0])
	}
	if csv != "" {
		t.Errorf("expected empty csv on error, got %q", csv)
	}
}

func TestConvertItem_MissingRequiredField(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "alpha",
		ItemData: map[string]interface{}{
			"alphaStr": "hello",
			// alphaInt and alphaDate missing — required, no default
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) == 0 {
		t.Fatal("expected errors for missing required fields")
	}
	if csv != "" {
		t.Errorf("expected empty csv on error, got %q", csv)
	}
	hasMissingErr := false
	for _, e := range errs {
		if errors.Is(e, ErrMissingRequiredField) {
			hasMissingErr = true
			break
		}
	}
	if !hasMissingErr {
		t.Errorf("expected ErrMissingRequiredField among errors: %v", errs)
	}
}

func TestConvertItem_WrongFieldType(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "beta",
		ItemData: map[string]interface{}{
			"betaStr":  "ok",
			"betaBool": "not-a-bool", // wrong type
			"betaReal": 1.0,
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) == 0 {
		t.Fatal("expected error for wrong field type")
	}
	if !errors.Is(errs[0], ErrInvalidFieldType) {
		t.Errorf("expected ErrInvalidFieldType, got %v", errs[0])
	}
	if csv != "" {
		t.Errorf("expected empty csv on error, got %q", csv)
	}
}

func TestConvertItem_InvalidDatetime(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "alpha",
		ItemData: map[string]interface{}{
			"alphaStr":  "hello",
			"alphaInt":  float64(1),
			"alphaDate": "not-a-date",
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) == 0 {
		t.Fatal("expected error for invalid datetime")
	}
	if csv != "" {
		t.Errorf("expected empty csv on error, got %q", csv)
	}
}

func TestConvertItem_InvalidDynamic(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "gamma",
		ItemData: map[string]interface{}{
			"gammaInt":     float64(1),
			"gammaStr":     "ok",
			"gammaDynamic": "{bad json}",
		},
	}
	csv, errs := ConvertItem(item, dl, fixedTimestamp)
	if len(errs) == 0 {
		t.Fatal("expected error for invalid dynamic JSON")
	}
	if !errors.Is(errs[0], ErrInvalidJSON) {
		t.Errorf("expected ErrInvalidJSON, got %v", errs[0])
	}
	if csv != "" {
		t.Errorf("expected empty csv on error, got %q", csv)
	}
}

func TestConvertItem_TimestampAutoGenerated(t *testing.T) {
	dl := buildDefinitionList(t)
	item := InputItem{
		ItemTypeName: "beta",
		ItemData: map[string]interface{}{
			"betaStr":  "x",
			"betaBool": false,
			"betaReal": 0.0,
		},
	}
	csv, errs := ConvertItem(item, dl, "") // empty timestamp → auto-generated
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if csv == "" {
		t.Fatal("expected non-empty csv")
	}
	// auto timestamp should be the first field in format YYYY-MM-DD HH:MM:SS.mmm
	parts := strings.SplitN(csv, ",", 2)
	if len(parts[0]) != len("2006-01-02 15:04:05.000") {
		t.Errorf("unexpected timestamp format: %q", parts[0])
	}
}
