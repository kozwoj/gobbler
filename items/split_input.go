package items

import (
	"encoding/json"
	"fmt"
)

type InputItem struct {
	ItemTypeName string
	ItemData     map[string]interface{}
}

/*
The input is an array of JSON objects, each representing an item described by its ItemDefinition. So the
structure of the input may look something like this:
[

	    {"allscalars":{"_string":"Hyundai","_boolean":true,"_datetime":"2023-10-01 12:00:00","_dynamic":"{\"color\":\"Crimson\",\"size\":\"small\"}","_int":0,"_real":12537.5948,"_timespan":"7d"}},
		{"vmShutdown": {"vmId": "vm_87345882", "shutdownStart": "2023-10-01 12:00:00.125", "shutdownReason": "OS update" }},
	    {"vmReboot":{"vmId": "vm_87345882", "eventTime": "2023-10-01 12:00:00.125", "rebootStart": "2023-10-01 11:00:00", "rebootDurationSec": "120s", "rebootReason": "OS update", "OS": "{\"os\": \"Windows\", \"version\": \"11\"}"}}

]

SplitInput function takes the input like the one above and returns a slice of InputItem objects, where
  - ItemTypeName is the name of the item type (e.g. "allscalars", "vmShutdown", "vmReboot")
  - ItemData is a map where the keys are the item property names (e.g. "_string", "_boolean", "_datetime", etc. for "allscalars"), and
    the values are the corresponding property values (e.g. "Hyundai", true, "2023-10-01 12:00:00", etc. for "allscalars")

The function tries to parse all items in the input array, and returns
  - slice of InputItem objects that are successfully parsed, and
  - a slice of errors for the items that failed to parse.
*/
func SplitInput(input []byte) ([]InputItem, []error) {
	// parseErrors collects errors for each item that failed to parse
	var parseErrors []error
	// verify that the input is a valid JSON array
	var itemsArray []map[string]json.RawMessage
	if err := json.Unmarshal(input, &itemsArray); err != nil {
		inputStr := string(input)
		parseErrors = append(parseErrors, NewSplitError(-1, inputStr, fmt.Errorf("%w: %v", ErrInvalidJSONArray, err)))
		return nil, parseErrors
	}
	splitItems := make([]InputItem, 0, len(itemsArray))
	for i, item := range itemsArray {
		// each item in the array should have exactly one key, which is the item type name
		if len(item) != 1 {
			// convert item to JSON for better error reporting
			itemJSON, _ := json.Marshal(item)
			parseErrors = append(parseErrors, NewSplitError(i, string(itemJSON), ErrInvalidItemStructure))
			continue
		}
		for itemTypeName, itemData := range item {
			var dataMap map[string]interface{}
			if err := json.Unmarshal(itemData, &dataMap); err != nil {
				parseErrors = append(parseErrors, NewSplitError(i, string(itemData), fmt.Errorf("%w for type %s: %v", ErrItemDataParsingFailed, itemTypeName, err)))
				continue
			}
			splitItems = append(splitItems, InputItem{
				ItemTypeName: itemTypeName,
				ItemData:     dataMap,
			})
		}
	}
	return splitItems, parseErrors
}
