package items

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

/* ConvertItem function takes an InputItem, its corresponding ItemDefinition from the DefinitionList, and a timestamp string,
validates the item against its definition, and if valid converts it to CSV string representation of its item field values.
The sequence of the values in the CSV string is determined by the order of the fields in the ItemDefinition. The function adds
the timestamp as the first value in the CSV string representing item field values.
The function returns an error if the item does not match its ItemDefinition, is missing required fields, or cannot be converted
to CSV.
Input parameters:
- input item
- item definition, and
- timestamp string
Output:
- CSV string representation of item field values, and
- error if the item does not match its ItemDefinition, is missing required fields, or cannot be converted to CSV.
*/

func ConvertItem(item InputItem, definitions DefinitionList, timestamp string) (string, []error) {
	// find the item definition for the item
	var errors []error
	itemDef, err := definitions.GetDefinition(item.ItemTypeName)
	if err != nil {
		errors = append(errors, NewConvertError(item.ItemTypeName, fmt.Sprintf("item type: %s", item.ItemTypeName), ErrItemTypeNotDefined))
		return "", errors
	}
	// iterate over fields in the item definition and validate/convert corresponding item values to their CSV values
	// collect the CSV values in csvValues slice
	csvValues := make([]string, len(itemDef.Columns))

	for i, column := range itemDef.Columns {
		value, exists := item.ItemData[column.Name]
		if exists {
			// check if value type is consistent with its item definition
			switch column.ValueType {
			case ColumnTypeBool: // value must be either true or false
				if val, ok := value.(bool); ok {
					csvValues[i] = fmt.Sprintf("%t", val)
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeDatetime: // value must be a string in the format YYYY-MM-DD HH:MM:SS.mmm
				if val, ok := value.(string); ok {
					// validate datetime format
					if err := CheckDateTime(val); err != nil {
						errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, val, fmt.Errorf("%w: %v", ErrInvalidDateTime, err)))
					} else {
						csvValues[i] = val
					}
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeDynamic: // value must be a string representing a JSON object
				if val, ok := value.(string); ok {
					// validate JSON format
					if !json.Valid([]byte(val)) {
						errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, val, ErrInvalidJSON))
					} else {
						// Escape the JSON string for CSV format (wrap in quotes and escape internal quotes)
						escapedVal := strings.ReplaceAll(val, `"`, `""`)
						csvValues[i] = `"` + escapedVal + `"`
					}
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeInt: // value must be an integer
				if val, ok := value.(float64); ok {
					// validate that the value is an integer
					if val != float64(int(val)) {
						errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%f", val), ErrInvalidFieldType))
					} else {
						csvValues[i] = fmt.Sprintf("%d", int(val))
					}
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeReal: // value must be a float
				if val, ok := value.(float64); ok {
					csvValues[i] = fmt.Sprintf("%g", val)
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeString: // value must be a string
				if val, ok := value.(string); ok {
					// Escape for CSV: if value contains comma, quote, or newline, wrap in quotes and escape internal quotes
					if strings.ContainsAny(val, `",`+"\n") {
						escaped := strings.ReplaceAll(val, `"`, `""`)
						csvValues[i] = `"` + escaped + `"`
					} else {
						csvValues[i] = val
					}
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			case ColumnTypeTimespan: // value must be a string representing a valid Go duration
				if val, ok := value.(string); ok {
					// validate timespan format
					if err := CheckTimeSpan(val); err != nil {
						errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, val, fmt.Errorf("%w: %v", ErrInvalidTimespan, err)))
					} else {
						csvValues[i] = val
					}
				} else {
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, fmt.Sprintf("%v", value), ErrInvalidFieldType))
				}
			} // end of switch
		} else {
			// if field value does not exist, check if it is required, and if there is a default value for it
			if !column.Optional {
				// the field is required, so check if there is a default value for it
				if column.DefaultValue != nil {
					// convert column.DefaultValue to string and add it to the csvValues slice
					csvValues[i] = fmt.Sprintf("%v", column.DefaultValue)
				} else {
					// if the field is required and does not have a default value, return an error
					errors = append(errors, NewFieldError(column.Name, item.ItemTypeName, "missing", ErrMissingRequiredField))
				}
			} else {
				csvValues[i] = "" // Use empty string for missing optional fields
			}
		}
	}

	// if any field errors were collected, return empty string — a partial CSV must not be written
	if len(errors) > 0 {
		return "", errors
	}

	// ToDo: decide whether to add \n to the CSV line

	// add timestamp to the CSV line as the first value
	if timestamp == "" {
		timestamp = time.Now().Format("2006-01-02 15:04:05.000")
	}
	csvValues = append([]string{timestamp}, csvValues...)

	// combine the values in the slice into a CSV line
	csvLine := strings.Join(csvValues, ",")

	return csvLine, errors
}
