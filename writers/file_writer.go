package writers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
)

const tickInterval = 500 * time.Millisecond
const defaultMaxAge = 60 * time.Minute

// FileWriter accumulates CSVitems in a buffer and flushes them to timestamped CSV
// files under a local directory. Files rotate when their age exceeds the item's Latency.
type FileWriter struct {
	buffer    []string
	file      *os.File
	outputDir string
	fileStart time.Time
	batchSize int
	maxAge    time.Duration
	typeName  string
	mu        sync.Mutex
}

// NewFileWriter creates a FileWriter for the given definition rooted at rootDir.
// batchSize controls how many CSV lines trigger an immediate flush.
// The subdirectory rootDir/def.Folder is created if it does not exist.
func NewFileWriter(rootDir string, def items.ItemDefinition, batchSize int) (*FileWriter, error) {
	outputDir := filepath.Join(rootDir, def.Folder)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("writers: create directory %s: %w", outputDir, err)
	}
	maxAge := time.Duration(def.Latency) * time.Minute
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}
	return &FileWriter{
		outputDir: outputDir,
		batchSize: batchSize,
		maxAge:    maxAge,
		typeName:  def.TypeName,
	}, nil
}

// Start launches the time-based flush goroutine. Call once before routing items here.
// On context cancellation the goroutine performs a final flush and closes the current file.
func (w *FileWriter) Start(ctx context.Context, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				w.mu.Lock()
				w.flush()
				if w.file != nil {
					w.file.Close()
					w.file = nil
				}
				w.mu.Unlock()
				return
			case <-ticker.C:
				w.mu.Lock()
				w.flush()
				w.mu.Unlock()
			}
		}
	}()
}

// Add is the pipeline.Worker handler. It appends the CSV line to the buffer
// and flushes immediately when the batch size threshold is reached.
func (w *FileWriter) Add(item pipeline.CSVitem) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, item.CSV)
	if len(w.buffer) >= w.batchSize {
		w.flush()
	}
}

// flush writes buffered lines to the current file, rotating to a new file when
// the current one has exceeded maxAge. Caller must hold mu.
func (w *FileWriter) flush() {
	if len(w.buffer) == 0 {
		return
	}
	rotate := w.file == nil || time.Since(w.fileStart) >= w.maxAge
	if rotate {
		if w.file != nil {
			w.file.Close()
			w.file = nil
		}
		fname := fmt.Sprintf("%s_%s.csv", time.Now().Format("2006-01-02_15-04-05"), w.typeName)
		f, err := os.OpenFile(filepath.Join(w.outputDir, fname), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// TODO: replace with structured logging
			fmt.Println("writers: FileWriter: open file:", err)
			return
		}
		w.file = f
		w.fileStart = time.Now()
	}
	if _, err := w.file.WriteString(strings.Join(w.buffer, "\n") + "\n"); err != nil {
		// TODO: replace with structured logging
		fmt.Println("writers: FileWriter: write:", err)
		return
	}
	w.buffer = nil
}
