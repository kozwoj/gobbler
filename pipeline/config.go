package pipeline

// StorageMode selects the writer backend for the pipeline.
type StorageMode string

const (
	StorageModeFile StorageMode = "file"
	StorageModeBlob StorageMode = "blob"
)

// Config holds the global configuration for the pipeline and its writers.
// Populated by the gobbler/pipeline/configure REST endpoint before the
// pipeline is started.
type Config struct {
	Mode            StorageMode
	OutputDir       string // file mode only
	AccountName     string // blob mode only
	AccountKey      string // blob mode only
	WriterQueueSize int
	WriterBatchSize int
}
