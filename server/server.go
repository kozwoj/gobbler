package server

import (
	"context"
	"fmt"
	"sync"

	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
	"github.com/kozwoj/gobbler/writers"
)

// Writer is the interface satisfied by both writers.FileWriter and writers.BlobWriter.
// It is defined here so the server package does not import writers directly for type
// assertions — only for construction (done inside route handlers).
type Writer interface {
	Start(ctx context.Context, wg *sync.WaitGroup)
	Add(item pipeline.CSVitem)
	Rotate()
	Stats() writers.WriterStats
}

// typeEntry bundles the live components for a single registered item type
// while the pipeline is running.
type typeEntry struct {
	writer Writer
	cancel context.CancelFunc // cancels this type's writer + worker goroutines
	wg     sync.WaitGroup     // tracks this type's goroutines for clean removal
}

// Server holds the mutable state of the Gobbler service.
// All exported methods are safe for concurrent use.
type Server struct {
	mu sync.RWMutex

	// configuration — set by gobbler/pipeline/configure
	config *pipeline.Config // nil = not yet configured

	// runtime — set by gobbler/pipeline/start
	running     bool
	pipelineCtx context.Context    // root context for all pipeline goroutines
	cancel      context.CancelFunc // cancels all pipeline goroutines
	wg          sync.WaitGroup     // tracks the dispatcher goroutine

	// item type registry — definitions are added/removed via gobbler/definition/*
	definitions items.DefinitionList

	// active type entries — populated when the pipeline is started
	types map[pipeline.ItemType]*typeEntry
}

// New creates a Server ready to accept configuration.
func New() *Server {
	return &Server{
		definitions: make(items.DefinitionList),
		types:       make(map[pipeline.ItemType]*typeEntry),
	}
}

// IsConfigured reports whether gobbler/pipeline/configure has been called successfully.
func (s *Server) IsConfigured() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config != nil
}

// IsRunning reports whether the pipeline has been started.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Config returns a copy of the current pipeline configuration,
// and false if the server has not been configured yet.
func (s *Server) Config() (pipeline.Config, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.config == nil {
		return pipeline.Config{}, false
	}
	return *s.config, true
}

// startType creates and starts the writer and worker goroutines for def.
// Must be called with s.mu write-locked and only after s.pipelineCtx has been set
// (which happens in handlePipelineStart before the startType loop, independently of s.running).
func (s *Server) startType(def items.ItemDefinition) error {
	ctx, cancel := context.WithCancel(s.pipelineCtx)
	entry := &typeEntry{cancel: cancel}

	var w Writer
	switch s.config.Mode {
	case pipeline.StorageModeFile:
		fw, err := writers.NewFileWriter(s.config.OutputDir, def, s.config.WriterBatchSize)
		if err != nil {
			cancel()
			return fmt.Errorf("start type %s: %w", def.TypeName, err)
		}
		w = fw
	case pipeline.StorageModeBlob:
		bw, err := writers.NewBlobWriter(pipeline.BlobConfig{
			AccountName: s.config.AccountName,
			AccountKey:  s.config.AccountKey,
		}, def, s.config.WriterBatchSize)
		if err != nil {
			cancel()
			return fmt.Errorf("start type %s: %w", def.TypeName, err)
		}
		w = bw
	default:
		cancel()
		return fmt.Errorf("start type %s: unknown storage mode %q", def.TypeName, s.config.Mode)
	}
	entry.writer = w

	w.Start(ctx, &entry.wg)
	worker := pipeline.NewWorker(ctx, &entry.wg, s.config.WriterQueueSize, w.Add)

	s.types[pipeline.ItemType(def.TypeName)] = entry
	pipeline.AddItemType(pipeline.ItemType(def.TypeName), &pipeline.TypeDescriptor{
		Definition: def,
		Queue:      worker.Queue,
	})
	return nil
}
