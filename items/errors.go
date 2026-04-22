package items

import (
	"errors"
	"fmt"
)

// Items-specific errors
var (
	ErrInvalidDateTime          = errors.New("invalid datetime format")
	ErrEmptyTimespan            = errors.New("timespan cannot be empty")
	ErrInvalidTimespan          = errors.New("invalid timespan format")
	ErrEmptyFileName            = errors.New("fileName cannot be empty")
	ErrInvalidFileName          = errors.New("invalid fileName format")
	ErrReservedName             = errors.New("\"timestamp\" is a reserved name")
	ErrMissingNameField         = errors.New("name field is missing or invalid")
	ErrInvalidDocumentation     = errors.New("documentation field is not a string")
	ErrInvalidFolderField       = errors.New("folder field is invalid")
	ErrNegativeLatency          = errors.New("latency cannot be negative")
	ErrInvalidLatency           = errors.New("latency field is not an integer")
	ErrEmptyColumns             = errors.New("orderedColumns field cannot be empty")
	ErrMissingColumns           = errors.New("orderedColumns field is missing or not an array")
	ErrInvalidColumnName        = errors.New("column name is missing or not a string")
	ErrUnsupportedColumnType    = errors.New("unsupported field/column type")
	ErrMissingColumnType        = errors.New("field type value missing or not a string")
	ErrInconsistentDefaultValue = errors.New("defaultValue inconsistent with column type")
	ErrInvalidDefaultDateTime   = errors.New("defaultValue is not correctly formed datetime")
)

// ItemsError provides context for item definition failures
type ItemsError struct {
	Op        string // operation: "validate", "parse", "create"
	Component string // component: "datetime", "timespan", "filename", "definition", "column"
	Field     string // field name being processed
	Value     string // field value for debugging
	Err       error  // underlying error
}

func (e *ItemsError) Error() string {
	if e.Field != "" && e.Value != "" {
		value := e.Value
		if len(value) > 30 {
			value = value[:27] + "..."
		}
		return fmt.Sprintf("items %s failed for %s field '%s' with value '%s': %v",
			e.Op, e.Component, e.Field, value, e.Err)
	} else if e.Field != "" {
		return fmt.Sprintf("items %s failed for %s field '%s': %v",
			e.Op, e.Component, e.Field, e.Err)
	}
	return fmt.Sprintf("items %s failed in %s: %v", e.Op, e.Component, e.Err)
}

func (e *ItemsError) Unwrap() error {
	return e.Err
}

// Helper functions to create common items errors
func NewValidationError(component, field, value string, err error) *ItemsError {
	return &ItemsError{
		Op:        "validate",
		Component: component,
		Field:     field,
		Value:     value,
		Err:       err,
	}
}

func NewParseError(component, field, value string, err error) *ItemsError {
	return &ItemsError{
		Op:        "parse",
		Component: component,
		Field:     field,
		Value:     value,
		Err:       err,
	}
}

func NewDefinitionError(field, value string, err error) *ItemsError {
	return &ItemsError{
		Op:        "create",
		Component: "definition",
		Field:     field,
		Value:     value,
		Err:       err,
	}
}

func NewColumnError(field, value string, err error) *ItemsError {
	return &ItemsError{
		Op:        "validate",
		Component: "column",
		Field:     field,
		Value:     value,
		Err:       err,
	}
}
