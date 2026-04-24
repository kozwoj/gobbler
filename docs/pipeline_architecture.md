**Ingestion Pipeline Architecture Discussion**

This document summarizes Gobbler's multi‑stage ingestion pipeline with per‑type workers, queues, batching, and file/blob rotation. 

**1\. Overview of the Pipeline**

The Gobbler processes incoming JSON items through a sequence of well‑defined stages:

- **JSON Parsing & Validation**
  - Performed inside the web service.
  - Invalid records are rejected immediately.
  - Valid records proceed to the next stage.
- **Conversion to CSV**
  - Still inside the web service.
  - Produces a compact, normalized representation of the item as CSV string
  - The sting is put into a struct with item type and placed in central input queue.
- **Central Dispatcher**
  - Reads from the input queue.
  - Determines the record type.
  - Routes each CSV string into the appropriate item type worker with a queue.
- **Per‑Type Worker**
  - All items of the same type are enqueued with a worker that handles that type. 
  - The worker has its now queue and is composed of a batcher followed by a writer.
  - Batcher accumulated CSV strings in a buffer
  - When the buffer reaches its defined size, it is passed to the writer
  - Writer appended the CSV records to a file/blob in the corresponding directory/container.
  - When a file/blob reaches a configured size, the writer rotates it.
  - This is the end of the ingestion pipeline.

All workers are functionally identical. The only difference between them is where they store the CSV encoded items. A specific worker stores enqueued items either in 
- name-and-timestamps-labeled files in a directory, or 
- name-and-timestamps-labeled blobs in a container. 

So we can think of the workers as two subtypes of the same object type: FileWorker and BlobWorker

**Design summary**

- Dispatcher only routes records.
- Each record type has its own queue and worker.
- Slow writers do not affect others.
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
                return
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
func (w *Worker[T]) Enqueue(item T) {
    select {
    case w.Queue <- item:
        // enqueued
    default:
        // queue full: apply backpressure or drop
    }
}
```
**3\. Dispatcher Pattern**

The dispatcher reads from the central input queue and routes items:
``` go
func Dispatch(rec any) {
    switch v := rec.(type) {
    case ARecord:
        workerA.Enqueue(v)
    case BRecord:
        workerB.Enqueue(v)
    }
}
```
This keeps the dispatcher lightweight and deterministic.

**4\. Worker Responsibilities: Batching and Rotation**

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

**5\. Optional Future Enhancements**

These are not required but align with micro‑pipeline principles:

- **Split worker into micro‑stages** (batcher → flusher → rotator)
- **Have batcher use rotating buffers** (when one buffer is appended to file the other is used for new items)
- **Add compression stage** (gzip, zstd, snappy).

## Connecting to the REST ingestion handler function

Gobbler exposes three REST endpoints
- endpoint to add new items type to routing table 
- endpoint to ingest a array of items (they cab be of different types), and
- endpoint to get Gobbler's status/statistics
  
```
REST → item-validation-conversion → multi-type queue → dispatcher → per-type queues → writers
                                                            ↑
                                                            |
                                                    atomic routing table
                                                            ↑
                                                            |
REST ---------→  definition-validation ---------→ type-designated-worker
```

The dispatcher is a goroutine with a read‑only view of the routing table, which maps item types to workers. The routing table is updated by an new type definition REST endpoint component.


```
Dispatcher:
table := atomicTable.Load()
q := (*table)[rec.Type]
```
1. Routing table type

Immutable routing table + atomic pointer swap

``` go
type RoutingTable map[ItemType]chan<- CSVitem
var routing atomic.Pointer[RoutingTable]
```
2. Dispatcher goroutine

``` go
func StartDispatcher(input <-chan CSVitem) {
    go func() {
        for item := range input {
            table := routing.Load()
            q := (*table)[item.Type]
            q <- rec
        }
    }()
}
```
This is the entire dispatcher. It never mutates anything.

3. REST endpoint adds a new type

``` go
func AddItemType(t ItemType, writer Writer) {
    old := routing.Load()
    newTable := make(RoutingTable, len(*old)+1)

    for k, v := range *old {
        newTable[k] = v
    }

    newTable[t] = writer.InputQueue()

    routing.Store(&newTable)
}
```

4. Only after this, REST accepts the new type

The REST handler logic (clean and safe)

4.1. Load routing table atomically
``` go
table := routing.Load()
```
4.2. Validate type has been defined and the writer for it has been created (is in routing table)
``` go
q, ok := (*table)[recordType]
if !ok {
    // reject request: type not supported
    http.Error(w, "unknown record type", http.StatusBadRequest)
    return
}
```
This ensures REST never accepts a type until the writer exists.

4.3 Validate JSON and encode CSV
(done ...)

4.4. Push to multi‑type queue
``` go
select {
case inputQueue <- Record{Type: recordType, CSV: csvString}:
    // success
default:
    // input queue full → apply backpressure policy
    http.Error(w, "ingestion overloaded", http.StatusServiceUnavailable)
}
```

## REST item definition endpoint

1. Receives type definition (JSON)
2. Parses and validate schema
3. Checks for duplicates/conflicts
4. Stores schema in registry
5. Creates per-type writer with a queue
6. Starts writer goroutine
7. Builds new routing table
8. Atomic pointer swaps

Item is what gets create after the JSON record has been validated and converted to CSV
``` go
type ItemType string

type Item struct {
    Type ItemType
    CSV  string
}
```

Unified registry of types descriptions and writer registry
``` go
type TypeDescriptor struct {
    Schema    *SchemaDefinition
    Validator Validator
    Queue     chan<- Record
    Writer    *Writer
}

type RoutingTable map[RecordType]*TypeDescriptor
var routing atomic.Pointer[RoutingTable]
```

This is how it is used in the dispatcher
``` go
table := routing.Load()
desc := (*table)[rec.Type]
desc.Queue <- rec
```

This is how it is used in REST handlers
``` go
table := routing.Load()
desc, ok := (*table)[RecordType(req.Type)]
if !ok {
    http.Error(w, "unknown type", http.StatusBadRequest)
    return
}

if err := desc.Validator.Validate(req.Body); err != nil {
    http.Error(w, "invalid record", http.StatusBadRequest)
    return
}

csv := encodeToCSV(req.Body)

select {
case inputQueue <- Record{Type: RecordType(req.Type), CSV: csv}:
    // success
default:
    http.Error(w, "ingestion overloaded", http.StatusServiceUnavailable)
}
```