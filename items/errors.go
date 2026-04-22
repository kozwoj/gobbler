package items

import (
	"errors"
	"fmt"
)

/* ============================  Items-specific errors ============================ */
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
	ErrFolderTooShort            = errors.New("folder name must be at least 3 characters")
	ErrDuplicateColumnName       = errors.New("duplicate column name")
	ErrDefinitionAlreadyExists   = errors.New("definition with this name already exists")
	ErrDefinitionNotFound        = errors.New("definition not found")
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

/* ============================  Ingest-specific errors ============================ */
var (
	ErrInvalidInputFormat    = errors.New("invalid input format")
	ErrEmptyInput            = errors.New("empty input provided")
	ErrUnsupportedItemType   = errors.New("unsupported item type")
	ErrParsingFailed         = errors.New("failed to parse input")
	ErrItemTypeNotDefined    = errors.New("item type not defined")
	ErrInvalidFieldType      = errors.New("invalid field type")
	ErrMissingRequiredField  = errors.New("missing required field")
	ErrInvalidJSON           = errors.New("invalid JSON format")
	ErrInvalidJSONArray      = errors.New("input is not a valid JSON array")
	ErrInvalidItemStructure  = errors.New("item does not have exactly one key")
	ErrItemDataParsingFailed = errors.New("item data cannot be parsed as JSON object")
)

// IngestError provides context for ingestion failures

type IngestError struct {
	Op       string // "parse", "validate", "convert"
	Input    string // truncated input for debugging
	ItemType string // type being processed
	Err      error  // underlying error
}

func (e *IngestError) Error() string {
	input := e.Input
	if len(input) > 50 {
		input = input[:47] + "..."
	}
	return fmt.Sprintf("ingest %s failed for item type '%s' with input '%s': %v",
		e.Op, e.ItemType, input, e.Err)
}

func (e *IngestError) Unwrap() error {
	return e.Err
}

/*
Helper functions
*/

func NewConvertError(itemType, input string, err error) *IngestError {
	return &IngestError{
		Op:       "convert",
		Input:    input,
		ItemType: itemType,
		Err:      err,
	}
}

func NewFieldError(fieldName, itemType, fieldValue string, err error) *IngestError {
	return &IngestError{
		Op:       "validate_field",
		Input:    fmt.Sprintf("field:%s value:%s", fieldName, fieldValue),
		ItemType: itemType,
		Err:      err,
	}
}

// Helper to create input splitting errors
func NewSplitError(itemIndex int, input string, err error) *IngestError {
	return &IngestError{
		Op:       "split",
		Input:    input,
		ItemType: fmt.Sprintf("item_%d", itemIndex),
		Err:      err,
	}
}