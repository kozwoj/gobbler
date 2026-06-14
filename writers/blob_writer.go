package writers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"

	gobblerclient "github.com/kozwoj/gobbler-client"
	"github.com/kozwoj/gobbler/items"
	"github.com/kozwoj/gobbler/pipeline"
)

// readSeekCloser wraps strings.Reader to satisfy io.ReadSeekCloser,
// which is required by the appendblob.Client.AppendBlock API.
type readSeekCloser struct {
	*strings.Reader
}

func (r readSeekCloser) Close() error { return nil }

// BlobWriter accumulates CSVitems in a buffer and flushes them to append blobs
// in an Azure Blob Storage container. The container name equals def.Folder.
// Blobs rotate when their age exceeds the item's Latency.
type BlobWriter struct {
	buffer          []string
	container       string
	accountName     string
	cred            *azblob.SharedKeyCredential
	blobClient      *appendblob.Client
	blobStart       time.Time
	writerBatchSize int
	maxAge          time.Duration
	typeName        string
	itemsWritten    int64
	lastFlush       time.Time
	currentBlob     string
	mu              sync.Mutex
	logger          gobblerclient.Client // always non-nil; Nop() when not configured
}

// NewBlobWriter creates a BlobWriter for the given definition and blob credentials.
// It creates the Azure container if it does not already exist and uploads a {typeName}.json
// blob describing the stored item structure.
// writerBatchSize controls how many CSV lines trigger an immediate flush.
func NewBlobWriter(cfg pipeline.BlobConfig, def items.ItemDefinition, writerBatchSize int) (*BlobWriter, error) {
	cred, err := azblob.NewSharedKeyCredential(cfg.AccountName, cfg.AccountKey)
	if err != nil {
		return nil, fmt.Errorf("writers: invalid blob credentials: %w", err)
	}

	serviceURL := fmt.Sprintf("https://%s.blob.core.windows.net/", cfg.AccountName)
	serviceClient, err := azblob.NewClientWithSharedKeyCredential(serviceURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("writers: create blob service client: %w", err)
	}

	containerClient := serviceClient.ServiceClient().NewContainerClient(def.Folder)
	_, err = containerClient.Create(context.Background(), nil)
	if err != nil && !strings.Contains(err.Error(), "ContainerAlreadyExists") {
		return nil, fmt.Errorf("writers: create container %s: %w", def.Folder, err)
	}

	schema, err := items.StoredItemDefinition(def)
	if err != nil {
		return nil, fmt.Errorf("writers: build %s.json for %s: %w", def.TypeName, def.TypeName, err)
	}
	typeJSONURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s.json", cfg.AccountName, def.Folder, def.TypeName)
	bbClient, err := blockblob.NewClientWithSharedKeyCredential(typeJSONURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("writers: create %s.json blob client for %s: %w", def.TypeName, def.TypeName, err)
	}
	if _, err = bbClient.UploadBuffer(context.Background(), schema, nil); err != nil {
		return nil, fmt.Errorf("writers: upload %s.json for %s: %w", def.TypeName, def.TypeName, err)
	}

	maxAge := time.Duration(def.Latency) * time.Minute
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}

	return &BlobWriter{
		container:       def.Folder,
		accountName:     cfg.AccountName,
		cred:            cred,
		writerBatchSize: writerBatchSize,
		maxAge:          maxAge,
		typeName:        def.TypeName,
		logger:          gobblerclient.Nop(),
	}, nil
}

// SetLogger injects a self-logging client into the writer. Must be called before
// Start. Safe to call concurrently; acquires the writer mutex.
func (w *BlobWriter) SetLogger(l gobblerclient.Client) {
	w.mu.Lock()
	w.logger = l
	w.mu.Unlock()
}

// Start launches the time-based flush goroutine. Call once before routing items here.
// On context cancellation the goroutine performs a final flush.
func (w *BlobWriter) Start(ctx context.Context, wg *sync.WaitGroup) {
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
func (w *BlobWriter) Add(item pipeline.CSVitem) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buffer = append(w.buffer, item.CSV)
	if len(w.buffer) >= w.writerBatchSize {
		w.flush()
	}
}

// Stats returns a point-in-time snapshot of the writer's operational state.
func (w *BlobWriter) Stats() WriterStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	return WriterStats{
		ItemsInBuffer: len(w.buffer),
		ItemsWritten:  w.itemsWritten,
		LastFlush:     w.lastFlush,
		CurrentOutput: w.currentBlob,
	}
}

// Rotate forces an immediate flush of the current blob and ensures the next write
// goes to a new timestamped blob. Safe to call from the management REST endpoint.
func (w *BlobWriter) Rotate() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flush() // flush whatever is buffered, creating the blob if needed
	// Nil the client so the next write opens a fresh blob.
	w.blobClient = nil
	w.blobStart = time.Time{}
	w.currentBlob = ""
}

// flush writes buffered lines to the current append blob, rotating to a new blob
// when the current one has exceeded maxAge. Caller must hold mu.
func (w *BlobWriter) flush() {
	if len(w.buffer) == 0 {
		return
	}
	ctx := context.Background()
	rotate := w.blobClient == nil || time.Since(w.blobStart) >= w.maxAge
	if rotate {
		w.blobStart = firstItemTime(w.buffer)
		blobName := fmt.Sprintf("%s_%s", w.blobStart.Format("2006-01-02_15-04-05.000"), w.typeName)
		blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s",
			w.accountName, w.container, blobName)
		client, err := appendblob.NewClientWithSharedKeyCredential(blobURL, w.cred, nil)
		if err != nil {
			_ = w.logger.Log("gobbler-writer-error", map[string]any{"itemType": w.typeName, "operation": "create-client", "errorMsg": err.Error()})
			return
		}
		if _, err = client.Create(ctx, nil); err != nil {
			_ = w.logger.Log("gobbler-writer-error", map[string]any{"itemType": w.typeName, "operation": "create-blob", "errorMsg": err.Error()})
			return
		}
		w.blobClient = client
		w.currentBlob = blobName
	}
	payload := strings.Join(w.buffer, "\n") + "\n"
	if _, err := w.blobClient.AppendBlock(ctx, readSeekCloser{strings.NewReader(payload)}, nil); err != nil {
		_ = w.logger.Log("gobbler-writer-error", map[string]any{"itemType": w.typeName, "operation": "append-block", "errorMsg": err.Error()})
		return
	}
	itemsCount := len(w.buffer)
	w.itemsWritten += int64(itemsCount)
	w.lastFlush = time.Now()
	w.buffer = nil
	_ = w.logger.Log("gobbler-writer-flush", map[string]any{"itemType": w.typeName, "itemsFlushed": itemsCount, "output": w.currentBlob})
}
