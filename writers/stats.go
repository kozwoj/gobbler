package writers

import "time"

// WriterStats is a point-in-time snapshot of a writer's operational state.
// Returned by the Stats() method on FileWriter and BlobWriter.
type WriterStats struct {
	ItemsInBuffer int       // number of CSV lines currently held in the buffer
	ItemsWritten  int64     // cumulative number of CSV lines successfully flushed since start
	LastFlush     time.Time // time of the last successful flush; zero if never flushed
	CurrentOutput string    // active file path or blob name; empty if none is currently open
}
