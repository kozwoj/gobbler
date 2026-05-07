package tester

import (
	_ "embed"
	"encoding/json"
)

//go:embed docs/testDefinitions.json
var testDefinitionsRaw []byte

//go:embed docs/itemDefinitionExamples.json
var itemDefinitionExamplesRaw []byte

// definitionIndex maps type name → raw JSON of the full definition object.
var definitionIndex map[string]json.RawMessage

func init() {
	definitionIndex = make(map[string]json.RawMessage)
	for _, raw := range [][]byte{testDefinitionsRaw, itemDefinitionExamplesRaw} {
		var defs []json.RawMessage
		if err := json.Unmarshal(raw, &defs); err != nil {
			panic("tester: failed to parse embedded definition JSON: " + err.Error())
		}
		for _, d := range defs {
			var header struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(d, &header); err == nil && header.Name != "" {
				definitionIndex[header.Name] = d
			}
		}
	}
}

// DefinitionJSON returns the raw JSON bytes for the named item type definition.
// The second return value is false if the type name is not found.
func DefinitionJSON(typeName string) ([]byte, bool) {
	d, ok := definitionIndex[typeName]
	return []byte(d), ok
}
