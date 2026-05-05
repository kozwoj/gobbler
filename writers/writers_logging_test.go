package writers

// Tests for Phase 2 Step 3: writer self-logging events.
// Uses a spyClient injected via SetLogger; exercises flush (via batch threshold)
// and error (via invalid output path) paths.

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	gobblerclient "github.com/kozwoj/gobbler-client"
	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
)

// ---- spy client ----

type writerLogCall struct {
	typeName string
	fields   map[string]any
}

type writerSpy struct {
	mu   sync.Mutex
	logs []writerLogCall
}

func (s *writerSpy) Log(typeName string, fields map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, writerLogCall{typeName: typeName, fields: fields})
	return nil
}
func (s *writerSpy) Flush() error            { return nil }
func (s *writerSpy) Close() error            { return nil }
func (s *writerSpy) SwapServer(string) error { return nil }

func (s *writerSpy) first(typeName string) (writerLogCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.logs {
		if c.typeName == typeName {
			return c, true
		}
	}
	return writerLogCall{}, false
}

var _ gobblerclient.Client = (*writerSpy)(nil) // compile-time interface check

// ---- helpers ----

// alphaDefForWriters is a minimal item definition for FileWriter tests.
func alphaDefForWriters(t *testing.T) items.ItemDefinition {
	t.Helper()
	var def items.ItemDefinition
	json := `{"name":"alpha","documentation":"test","folder":"alpha","latencyMinutes":1,` +
		`"orderedColumns":[{"name":"alphaStr","type":"string"},{"name":"alphaInt","type":"int"}]}`
	if err := items.CreateItemDefinition(json, &def); err != nil {
		t.Fatalf("alphaDefForWriters: %v", err)
	}
	return def
}

func createTestContext() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// closeFileWriter closes fw's open file handle so TempDir cleanup can remove it.
func closeFileWriter(t *testing.T, fw *FileWriter) {
	t.Helper()
	t.Cleanup(func() {
		fw.mu.Lock()
		defer fw.mu.Unlock()
		if fw.file != nil {
			_ = fw.file.Close()
			fw.file = nil
		}
	})
}

// ---- FileWriter tests ----

// WL1: Batch-size flush emits gobbler-writer-flush with correct fields.
func TestWL1_FileWriter_FlushEvent(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 1) // batchSize=1 → flush on first Add
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	closeFileWriter(t, fw)
	spy := &writerSpy{}
	fw.SetLogger(spy)

	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "hello,1"})

	call, ok := spy.first("gobbler-writer-flush")
	if !ok {
		t.Fatal("expected gobbler-writer-flush log call, got none")
	}
	if call.fields["typeName"] != "alpha" {
		t.Errorf("typeName = %v, want alpha", call.fields["typeName"])
	}
	if call.fields["itemsFlushed"] != 1 {
		t.Errorf("itemsFlushed = %v, want 1", call.fields["itemsFlushed"])
	}
	if call.fields["output"] == "" {
		t.Error("output field is empty, want file path")
	}
}

// WL2: Multiple items flushed in one batch; itemsFlushed reflects the batch size.
func TestWL2_FileWriter_FlushEvent_BatchOf3(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 3) // batchSize=3
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	closeFileWriter(t, fw)
	spy := &writerSpy{}
	fw.SetLogger(spy)

	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "a,1"})
	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "b,2"})
	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "c,3"}) // triggers flush

	call, ok := spy.first("gobbler-writer-flush")
	if !ok {
		t.Fatal("expected gobbler-writer-flush log call, got none")
	}
	if call.fields["itemsFlushed"] != 3 {
		t.Errorf("itemsFlushed = %v, want 3", call.fields["itemsFlushed"])
	}
}

// WL3: Nop logger (default) — no panic when flush fires.
func TestWL3_FileWriter_NopLogger_NoFlushEvent(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 1)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	closeFileWriter(t, fw)
	// Do NOT call SetLogger — Nop() is the default.
	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "x,1"})
	// No assertion needed — just verifying no panic occurs.
}

// WL4: Open-file error path emits gobbler-writer-error with operation="open-file".
// We trigger this by redirecting outputDir to a regular file so os.OpenFile fails.
func TestWL4_FileWriter_OpenFileError(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 1)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	spy := &writerSpy{}
	fw.SetLogger(spy)

	// Create a regular file that will block OpenFile(outputDir/file.csv) by using
	// that file's path as the directory name.
	blockingPath := outputDir + "/blocker"
	if err2 := os.WriteFile(blockingPath, []byte("x"), 0644); err2 != nil {
		t.Skip("could not create blocking file: " + err2.Error())
	}
	fw.outputDir = blockingPath // now points to a file, not a directory

	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "fail,1"})

	call, ok := spy.first("gobbler-writer-error")
	if !ok {
		t.Fatal("expected gobbler-writer-error log call, got none")
	}
	if call.fields["operation"] != "open-file" {
		t.Errorf("operation = %v, want open-file", call.fields["operation"])
	}
	if call.fields["typeName"] != "alpha" {
		t.Errorf("typeName = %v, want alpha", call.fields["typeName"])
	}
	if call.fields["error"] == "" {
		t.Error("error field is empty")
	}
}

// WL5: Rotate flushes buffered items and emits gobbler-writer-flush.
func TestWL5_FileWriter_RotateEmitsFlush(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 100) // high batch size — won't auto-flush
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	spy := &writerSpy{}
	fw.SetLogger(spy)

	// Pre-load the buffer directly (same package, unexported field).
	fw.buffer = []string{"a,1", "b,2"}
	fw.Rotate()

	call, ok := spy.first("gobbler-writer-flush")
	if !ok {
		t.Fatal("expected gobbler-writer-flush after Rotate, got none")
	}
	if call.fields["itemsFlushed"] != 2 {
		t.Errorf("itemsFlushed = %v, want 2", call.fields["itemsFlushed"])
	}
}

// WL6: Ticker-driven flush (via Start goroutine) also emits gobbler-writer-flush.
func TestWL6_FileWriter_TickerFlushEvent(t *testing.T) {
	outputDir := t.TempDir()
	def := alphaDefForWriters(t)
	fw, err := NewFileWriter(outputDir, def, 100)
	if err != nil {
		t.Fatalf("NewFileWriter: %v", err)
	}
	spy := &writerSpy{}
	fw.SetLogger(spy)

	var wg sync.WaitGroup
	ctx, cancel := createTestContext()
	fw.Start(ctx, &wg)

	// Add one item so there is something to flush when the ticker fires.
	fw.Add(pipeline.CSVitem{Type: "alpha", CSV: "tick,1"})

	// Wait up to 2× tickInterval for the flush event.
	deadline := time.Now().Add(2 * tickInterval)
	for time.Now().Before(deadline) {
		if _, ok := spy.first("gobbler-writer-flush"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if _, ok := spy.first("gobbler-writer-flush"); !ok {
		t.Fatal("expected gobbler-writer-flush from ticker goroutine, got none")
	}
}
