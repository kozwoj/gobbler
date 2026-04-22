package writers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/appendblob"

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
	buffer      []string
	container   string
	accountName string
	cred        *azblob.SharedKeyCredential
	blobClient  *appendblob.Client
	blobStart   time.Time
	batchSize   int
	maxAge      time.Duration
	typeName    string
	mu          sync.Mutex
}

// NewBlobWriter creates a BlobWriter for the given definition and blob credentials.
// It creates the Azure container if it does not already exist.
// batchSize controls how many CSV lines trigger an immediate flush.
func NewBlobWriter(cfg pipeline.BlobConfig, def items.ItemDefinition, batchSize int) (*BlobWriter, error) {
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

	maxAge := time.Duration(def.Latency) * time.Minute
	if maxAge == 0 {
		maxAge = defaultMaxAge
	}

	return &BlobWriter{
		container:   def.Folder,
		accountName: cfg.AccountName,
		cred:        cred,
		batchSize:   batchSize,
		maxAge:      maxAge,
		typeName:    def.TypeName,
	}, nil
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
	if len(w.buffer) >= w.batchSize {
		w.flush()
	}
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
		w.blobStart = time.Now()
		blobName := fmt.Sprintf("%s_%s", w.blobStart.Format("2006-01-02_15-04-05"), w.typeName)
		blobURL := fmt.Sprintf("https://%s.blob.core.windows.net/%s/%s",
			w.accountName, w.container, blobName)
		client, err := appendblob.NewClientWithSharedKeyCredential(blobURL, w.cred, nil)
		if err != nil {
			// TODO: replace with structured logging
			fmt.Println("writers: BlobWriter: create client:", err)
			return
		}
		if _, err = client.Create(ctx, nil); err != nil {
			// TODO: replace with structured logging
			fmt.Println("writers: BlobWriter: create blob:", err)
			return
		}
		w.blobClient = client
	}
	payload := strings.Join(w.buffer, "\n") + "\n"
	if _, err := w.blobClient.AppendBlock(ctx, readSeekCloser{strings.NewReader(payload)}, nil); err != nil {
		// TODO: replace with structured logging
		fmt.Println("writers: BlobWriter: append:", err)
		return
	}
	w.buffer = nil
}
