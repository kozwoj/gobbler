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

	// InstanceName identifies this Gobbler instance in telemetry emitted to a
	// logging Gobbler server. Required when LoggerEndpoint is set; must be
	// unique across all instances sharing the same logging server.
	InstanceName string

	// Optional self-logging: when LoggerEndpoint is non-empty the server
	// constructs a gobbler-client that ships its own operational events to a
	// separate Gobbler instance for analysis.
	LoggerEndpoint      string   // URL of the receiving Gobbler server, e.g. "http://host:8080"
	LoggerTypes         []string // item type names the logger will emit
	LoggerBatchSize     int      // client batch size; 0 means use client default (100)
	LoggerFlushInterval string   // Go duration string, e.g. "30s"; "" means use client default (10s)
}
