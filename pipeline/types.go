package pipeline

import "github.com/kozwoj/gobbler/items"

// ItemType identifies a registered item type by name.
type ItemType string

// CSVitem is what travels through the pipeline after validation and CSV conversion.
// It carries the item type name and the CSV-encoded record.
type CSVitem struct {
	Type ItemType
	CSV  string
}

// WriterKind distinguishes file-based from blob-based writers.
type WriterKind int

const (
	WriterKindFile WriterKind = iota
	WriterKindBlob
)

// BlobConfig holds the Azure Blob Storage credentials for a blob writer.
// The target container name is taken from the ItemDefinition.Folder field.
type BlobConfig struct {
	AccountName string
	AccountKey  string
}

// WriterConfig carries the storage configuration for a writer.
// For file writers only Kind is needed; for blob writers Blob must be set.
type WriterConfig struct {
	Kind WriterKind
	Blob *BlobConfig // non-nil when Kind == WriterKindBlob
}

// TypeDescriptor ties together an item definition, its writer's input channel,
// and the storage configuration needed by the writer.
type TypeDescriptor struct {
	Definition items.ItemDefinition
	Queue      chan<- CSVitem
	Config     WriterConfig
}

// RoutingTable maps each registered ItemType to its TypeDescriptor.
type RoutingTable map[ItemType]*TypeDescriptor
