**Ingestion Pipeline Architecture**

This document summarizes Gobbler's ingestion pipeline with per‑type workers, queues, batching, and file/blob rotation.

**1\. Overview of the Pipeline**

The Gobbler processes incoming JSON items through a sequence of well‑defined stages:

- **JSON Parsing & Validation**
  - Performed inside the ingest route handler.
  - Invalid records are rejected immediately and returned to the caller.
  - Valid records proceed to the next stage.
- **Conversion to CSV**
  - Still inside the ingest route handler.
  - Produces a compact, normalized representation of the item as a CSV string.
  - The string is wrapped in a `CSVitem` struct (type name + CSV) and sent directly to the appropriate per‑type worker queue.
- **Per‑Type Worker**
  - Each registered item type has its own bounded queue and goroutine.
  - The ingest handler looks up the type's `TypeDescriptor` via an atomic routing table (`pipeline.LookupType`) and does a non‑blocking send into the worker's queue.
  - If the queue is full the item is rejected immediately and reported back to the caller — no silent drops.
  - The worker goroutine calls the writer's `Add` method for each dequeued `CSVitem`.
  - The writer accumulates CSV strings in a batch buffer; when the buffer reaches `batchSize` it is flushed to a file or blob.
  - When a file/blob reaches its configured size the writer rotates it (closes the current file/blob and opens a new one).
  - This is the end of the ingestion pipeline.

All workers are functionally identical. The only difference between them is where they store the CSV‑encoded items. A worker stores items either in:
- name-and-timestamp‑labeled files in a directory (`FileWriter`), or
- name-and-timestamp‑labeled blobs in a container (`BlobWriter`).

**Design summary**

- No central queue, no dispatcher goroutine.
- The ingest handler routes records directly using the atomic routing table.
- Each record type has its own queue and worker.
- A full worker queue causes an immediate, per‑type rejection — the client is always informed.
- Slow writers do not affect other types.
- Predictable latency and throughput.
- Natural isolation and backpressure.

**2\. Worker Pattern in Go**

A worker is defined by:

- A bounded queue (chan T).
- A goroutine that processes items.
- A context for cancellation.
- A handler function that performs the work of item batching, writing batches and rotating files/blobs.

**Worker Structure**
``` go
type Worker[T any] struct {
    Queue chan T
    ctx   context.Context
    wg    *sync.WaitGroup
}
```

**Worker Creation**
``` go
func NewWorker[T any](ctx context.Context, wg *sync.WaitGroup, queueSize int, handler func(T)) *Worker[T] {
    w := &Worker[T]{
        Queue: make(chan T, queueSize),
        ctx:   ctx,
        wg:    wg,
    }

    wg.Add(1)
    go func() {
        defer wg.Done()
        for {
            select {
            case <-ctx.Done():
                // Drain any items already in the queue before exiting so that
                // the writer's buffer receives everything enqueued before shutdown.
                for {
                    select {
                    case item := <-w.Queue:
                        handler(item)
                    default:
                        return
                    }
                }
            case item := <-w.Queue:
                handler(item)
            }
        }
    }()

    return w
}
```
**Enqueue Logic**
``` go
// Enqueue adds item to the worker's queue without blocking.
// Returns false if the queue is full.
func (w *Worker[T]) Enqueue(item T) bool {
    select {
    case w.Queue <- item:
        return true
    default:
        return false
    }
}
```
**3\. Worker Responsibilities: Batching and Rotation**

Each worker:

- Accumulates CSV strings into a batch.
- Flushes the batch to a file/blob when:
  - batch size threshold is reached, or
  - a flush timeout occurs.
- Tracks file/blob size.
- Rotates when the size limit is reached.

### Note

This stage is analogous to the **extent writer** in Kusto. Kusto ingestion is built from small, isolated stages, each with its own queue and worker. This pipeline maps cleanly to this model:

| **Kusto Stage** | **Your Stage** |
| --- | --- |
| Parsing | JSON parsing |
| Validation | JSON validation |
| Shaping | CSV conversion |
| Encoding | CSV already encoded |
| Compression | (optional) |
| Extent Writer | Worker batching + rotation |

The combination of per‑type queues, per‑type workers, batching, and rotation gives a robust ingestion pipeline that can evolve into more advanced forms if needed.

**4\. Optional Future Enhancements**

These are not required but align with micro‑pipeline principles:

- **Split worker into micro‑stages** (batcher → flusher → rotator)
- **Have batcher use rotating buffers** (when one buffer is appended to file the other is used for new items)
- **Add compression stage** (gzip, zstd, snappy).

## Connecting to the REST ingest handler

Gobbler exposes REST endpoints for managing item type definitions and ingesting items. The ingest handler routes items directly — there is no dispatcher goroutine.

```
POST /gobbler/ingest
    → JSON parsing & validation
    → CSV conversion
    → LookupType (atomic routing table load)
    → non-blocking send to desc.Queue
    → per-type worker goroutine
    → writer (FileWriter or BlobWriter)

POST /gobbler/definition/add
    → definition validation
    → AddItemType (atomic routing table swap)
    → NewWorker + writer.Start
```

The routing table maps each registered `ItemType` to a `TypeDescriptor` that holds the worker's queue channel. The ingest handler loads the table atomically, looks up the type, and does a non‑blocking send:

``` go
type RoutingTable map[ItemType]*TypeDescriptor
var routing atomic.Pointer[RoutingTable]
```

``` go
// Ingest handler — direct routing
desc := pipeline.LookupType(pipeline.ItemType(item.ItemTypeName))
if desc == nil {
    // type not registered → rejected
}
select {
case desc.Queue <- pipeline.CSVitem{Type: ..., CSV: csvStr}:
    ingested++
default:
    // worker queue full → rejected with typeName and reason
}
```

Adding a new type atomically swaps in a new routing table so the ingest handler never observes a partial update:

``` go
func AddItemType(t ItemType, desc *TypeDescriptor) {
    old := routing.Load()
    newTable := make(RoutingTable, len(*old)+1)
    for k, v := range *old {
        newTable[k] = v
    }
    newTable[t] = desc
    routing.Store(&newTable)
}
```

