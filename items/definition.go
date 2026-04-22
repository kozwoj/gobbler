package items

import (
	"encoding/json"
	"fmt"
	"time"
)

/*
ItemDefinition describes structure of logged item type. It is created by parsing JSON
representation of item definition described in docs/itemsSchema.json file.
*/
type ItemDefinition struct {
	TypeName      string
	Documentation string
	Folder        string
	Latency       int // latency in minutes
	Columns       []Column
}

type Column struct {
	Name         string      // name of the column
	ValueType    ColumnType  // type of the column, one of the supported types listed in ColumnTypesMap
	DefaultValue interface{} // default value of the field, if not provided, it is set to nil
	Optional     bool        // if true, the field is optional, otherwise it is required
}

// ColumnType identifies the type of a column value
type ColumnType int

const (
	ColumnTypeBool     ColumnType = iota // "bool"
	ColumnTypeDatetime                   // "datetime" - input format: YYYY-MM-DD HH:MM:SS.mmm
	ColumnTypeDynamic                    // "dynamic"  - input format: { JSON representation of dynamic object }
	ColumnTypeInt                        // "int"
	ColumnTypeReal                       // "real"     - input format: 0.0
	ColumnTypeString                     // "string"
	ColumnTypeTimespan                   // "timespan" - Go duration format e.g. "1h10m10s"
)

// ColumnTypesMap maps ColumnType to its JSON type name
var ColumnTypesMap = map[ColumnType]string{
	ColumnTypeBool:     "bool",
	ColumnTypeDatetime: "datetime",
	ColumnTypeDynamic:  "dynamic",
	ColumnTypeInt:      "int",
	ColumnTypeReal:     "real",
	ColumnTypeString:   "string",
	ColumnTypeTimespan: "timespan",
}

/*
CreateItemDefinition is a constructor that parses JSON representation of item definition and creates
corresponding instance of ItemDefinition struct. The functions takes
- def: JSON representation of item definition as string
- itemDef: pointer to empty ItemDefinition to be instantiated
and returns
- error: nil if the ItemDefinition is instantiated successfully, otherwise an error object
*/
func CreateItemDefinition(def string, itemDef *ItemDefinition) error {
	// unmarshal json object into a map
	m := make(map[string]interface{})
	e := json.Unmarshal([]byte(def), &m)
	if e != nil {
		return e
	}
	/*
		parse initial fields of the definition: name, documentation, folder, and latency
	*/
	// check if definition contains required name field of type string
	if name, ok := m["name"].(string); ok {
		// check if the name can be used to create a directory/file name
		if err := CheckFileName(name); err == nil {
			// check that the name is not reserved name "timestamp"
			if name == "timestamp" {
				return NewDefinitionError("name", name, ErrReservedName)
			}
			itemDef.TypeName = name
		} else {
			return NewDefinitionError("name", name, ErrInvalidFileName)
		}
	} else {
		return NewDefinitionError("name", "", ErrMissingNameField)
	}
	// check if definition contains documentation field of type string, and set it to empty string if not present
	if doc, ok := m["documentation"].(string); ok {
		itemDef.Documentation = doc
	} else {
		if m["documentation"] != nil {
			return NewDefinitionError("documentation", fmt.Sprintf("%v", m["documentation"]), ErrInvalidDocumentation)
		}
		itemDef.Documentation = ""
	}
	// check if definition contains optional folder field of type string, and if it is missing set it to the value of the name field
	if folder, ok := m["folder"].(string); ok {
		// check if the folder is a valid file name and meets the minimum length of 3
		if err := CheckFileName(folder); err == nil {
			if len(folder) < 3 {
				return NewDefinitionError("folder", folder, ErrFolderTooShort)
			}
			itemDef.Folder = folder
		} else {
			return NewDefinitionError("folder", folder, ErrInvalidFolderField)
		}
	} else {
		if m["folder"] != nil {
			return NewDefinitionError("folder", fmt.Sprintf("%v", m["folder"]), ErrInvalidFolderField)
		}
		itemDef.Folder = itemDef.TypeName
	}
	// check definition contains latencyMinutes field of type integer, and if it is missing set it to default value 1
	// JSON numbers unmarshal as float64, so accept float64 and verify it has no fractional part
	if latencyRaw, ok := m["latencyMinutes"].(float64); ok {
		if latencyRaw != float64(int(latencyRaw)) {
			return NewDefinitionError("latencyMinutes", fmt.Sprintf("%v", latencyRaw), ErrInvalidLatency)
		}
		if latencyRaw < 0 {
			return NewDefinitionError("latencyMinutes", fmt.Sprintf("%v", latencyRaw), ErrNegativeLatency)
		}
		itemDef.Latency = int(latencyRaw)
	} else { // if latencyMinutes field is not present or not of type integer, set it to default value of 1
		if m["latencyMinutes"] != nil {
			return NewDefinitionError("latencyMinutes", fmt.Sprintf("%v", m["latencyMinutes"]), ErrInvalidLatency)
		}
		itemDef.Latency = 1 // set latency to default value of 1
	}
	// check if the definition contains orderedColumns field of type array
	if columns, ok := m["orderedColumns"].([]interface{}); ok {
		// check if the columns array is empty
		if len(columns) == 0 {
			return NewDefinitionError("orderedColumns", "[]", ErrEmptyColumns)
		}
		// iterate over each column in the array and create corresponding Column instance
		columnNames := make(map[string]struct{}, len(columns))
		for _, col := range columns {
			cld, err := GetColumnDefinition(col)
			if err != nil {
				return err
			}
			if _, exists := columnNames[cld.Name]; exists {
				return NewDefinitionError("orderedColumns", cld.Name, ErrDuplicateColumnName)
			}
			columnNames[cld.Name] = struct{}{}
			itemDef.Columns = append(itemDef.Columns, cld) // add column to columns slice of ItemDefinition
		}
	} else { // orderedColumns field is not present or not of type array
		return NewDefinitionError("orderedColumns", fmt.Sprintf("%v", m["orderedColumns"]), ErrMissingColumns)
	}
	return nil
}

/*
GetColumnDefinition parses JSON representation of column definition and returns corresponding Column instance.
*/
func GetColumnDefinition(column interface{}) (Column, error) {
	// create a new Column instance to hold the column definition
	cld := Column{}
	// check if column is of type map[string]interface{} that is, object holding key-value pairs
	if colMap, ok := column.(map[string]interface{}); ok {

		// check if the colMap contains name field of type string
		if name, ok := colMap["name"].(string); ok {
			if name == "timestamp" {
				return cld, NewColumnError("name", name, ErrReservedName)
			}
			cld.Name = name
		} else {
			return cld, NewColumnError("name", fmt.Sprintf("%v", colMap["name"]), ErrInvalidColumnName)
		}

		// check if colMap contains field "valueType" of type string and if the string is one of the supported types listed in ColumnTypesMap
		if valueType, ok := colMap["type"].(string); ok {
			// iterate over each key-value pair in ColumnTypesMap and check if the valueType matches any of the keys
			found := false
			for k, v := range ColumnTypesMap {
				if v == valueType {
					cld.ValueType = k // set valueType field of Column instance to key corresponding to valueType string
					found = true
					break
				}
			}
			if !found {
				return cld, NewColumnError("type", valueType, ErrUnsupportedColumnType)
			}
		} else {
			return cld, NewColumnError("type", fmt.Sprintf("%v", colMap["type"]), ErrMissingColumnType)
		}

		// check if the colMap contains field "defaultValue", and if its type is consistent with valueType field
		if dfv, ok := colMap["defaultValue"]; ok {
			switch cld.ValueType {
			case ColumnTypeBool:
				if _, ok := dfv.(bool); ok {
					cld.DefaultValue = dfv.(bool)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeDatetime:
				if _, ok := dfv.(string); ok {
					if err := CheckDateTime(dfv.(string)); err != nil {
						return cld, NewColumnError("defaultValue", dfv.(string), ErrInconsistentDefaultValue)
					}
					cld.DefaultValue = dfv.(string)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeDynamic:
				if _, ok := dfv.(string); ok {
					// parse the string to check if it is a valid JSON object
					if err := json.Unmarshal([]byte(dfv.(string)), &map[string]interface{}{}); err != nil {
						return cld, NewColumnError("defaultValue", dfv.(string), ErrInconsistentDefaultValue)
					}
					cld.DefaultValue = dfv.(string)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeInt: // JSON numbers unmarshal as float64, so we must accept float64 and verify it has no fractional part
				if f, ok := dfv.(float64); ok {
					if f != float64(int64(f)) {
						return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
					}
					cld.DefaultValue = int(f)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeReal:
				if _, ok := dfv.(float64); ok {
					cld.DefaultValue = dfv.(float64)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeString:
				if _, ok := dfv.(string); ok {
					cld.DefaultValue = dfv.(string)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			case ColumnTypeTimespan:
				if _, ok := dfv.(string); ok {
					// parse the string to check if it is in the correct timespan format
					if err := CheckTimeSpan(dfv.(string)); err != nil {
						return cld, NewColumnError("defaultValue", dfv.(string), ErrInconsistentDefaultValue)
					}
					cld.DefaultValue = dfv.(string)
				} else {
					return cld, NewColumnError("defaultValue", fmt.Sprintf("%v", dfv), ErrInconsistentDefaultValue)
				}
			default: // invalid valueType
				return cld, NewColumnError("type", fmt.Sprintf("%d", cld.ValueType), ErrUnsupportedColumnType)
			}
		} else { // if defaultValue field is not present, set it to nil
			cld.DefaultValue = nil
		}

		// check if the colMap contains field "optional" of type bool. If optional field is not present, set it to false
		if opt, ok := colMap["optional"].(bool); ok {
			cld.Optional = opt
		} else {
			if colMap["optional"] != nil {
				return cld, NewColumnError("optional", fmt.Sprintf("%v", colMap["optional"]), ErrInconsistentDefaultValue)
			}
			cld.Optional = false
		}
	}
	return cld, nil
}

/* ========================================== HELPER FUNCTIONS ========================================== */

func CheckDateTime(dateTime string) error {
	// parse the dateTime string to check if it is in the correct format
	withMilliseconds := "2006-01-02 15:04:05.000"
	withoutMilliseconds := "2006-01-02 15:04:05"
	// try with milliseconds first
	_, err := time.Parse(withMilliseconds, dateTime)
	if err == nil {
		return nil
	}
	// try without milliseconds
	_, err = time.Parse(withoutMilliseconds, dateTime)
	if err != nil {
		return NewValidationError("datetime", "dateTime", dateTime, ErrInvalidDateTime)
	}
	return nil
}

/*
TimeSpan should be a non empty string, which is represents time.Duration in Go
e.g. "2.5h", "3m", "4s", "5ms", "1h10m10s". !! Go does not support days as time unit.
*/
func CheckTimeSpan(timeSpan string) error {
	if timeSpan == "" {
		return NewValidationError("timespan", "timeSpan", timeSpan, ErrEmptyTimespan)
	}
	_, err := time.ParseDuration(timeSpan)
	if err != nil {
		return NewValidationError("timespan", "timeSpan", timeSpan, fmt.Errorf("%w: %v", ErrInvalidTimespan, err))
	}
	return nil
}

/*
Both item name and folder name are used to create directory/container and file/blob names, so they should
- start with character, and
- not contain the following characters { "/","\",":","*","?"," ","<",">","|" }
*/
func CheckFileName(fileName string) error {
	// check if the fileName string is empty
	if fileName == "" {
		return NewValidationError("filename", "fileName", fileName, ErrEmptyFileName)
	}
	// check if fileName string starts with a character (a-z, A-Z)
	if (fileName[0] < 'a' || fileName[0] > 'z') && (fileName[0] < 'A' || fileName[0] > 'Z') {
		return NewValidationError("filename", "fileName", fileName, fmt.Errorf("%w: must start with a character", ErrInvalidFileName))
	}
	// check if fileName string contains any of the invalid characters
	for _, c := range fileName {
		if c == '/' || c == '\\' || c == ':' || c == '*' || c == '?' || c == ' ' || c == '<' || c == '>' || c == '|' {
			return NewValidationError("filename", "fileName", fileName, fmt.Errorf("%w: contains invalid character %c", ErrInvalidFileName, c))
		}
	}
	return nil // valid fileName format
}
