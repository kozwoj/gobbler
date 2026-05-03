# Server–Pipeline–Writer Wiring

This note describes how the REST server, per-type workers, and writers are connected at startup and at runtime.

---

## Startup sequence

```
main()
 │
 └─ httpServer.ListenAndServe(...)   // REST endpoints ready
```

No item types are registered at startup; the ingest endpoint will reject all types until definitions are added and the pipeline is started.

---

## Registering a new item type (definition endpoint)

The REST definition endpoint performs these steps in order:

```
1. items.CreateItemDefinition(body)          // parse + validate JSON schema → ItemDefinition

2. items.DefinitionList.AddDefinition(def)   // store in the items registry

3. writer := writers.NewFileWriter(rootDir, def, writerBatchSize)   // file writer  ─┐ pick one
   or writers.NewBlobWriter(blobCfg, def, writerBatchSize)     // blob writer  ─┘

4. writer.Start(ctx, wg)                     // start time-based flush goroutine

5. worker := pipeline.NewWorker(ctx, wg, perTypeQueueSize, writer.Add)
   // generic Worker[CSVitem]; its goroutine calls writer.Add for every dequeued item

6. desc := &pipeline.TypeDescriptor{
       Definition: def,
       Queue:      worker.Queue,   // send-only channel exposed by the worker
       Config:     writerConfig,   // WriterKindFile or WriterKindBlob + BlobConfig
   }

7. pipeline.AddItemType(pipeline.ItemType(def.TypeName), desc)
   // atomic pointer swap → ingest endpoint now recognises this type
```

After step 7 the ingest endpoint will accept items of this type.

---

## Ingesting items (ingest endpoint)

```
POST /gobbler/ingest  (body: JSON array of {"typeName": {...}} objects)
 │
 ├─ items.SplitInput(body)                        // split array into []InputItem
 │
 ├─ for each InputItem:
 │   ├─ csv, errs := items.ConvertItem(item, definitions, timestamp)
 │   │                                             // validate + convert to CSV string
 │   ├─ desc := pipeline.LookupType(typeName)     // nil → rejected (type not registered)
 │   └─ non-blocking send to desc.Queue           // full → rejected (worker queue full)
 │          CSVitem{Type: typeName, CSV: csv}
 │
 └─ 200 OK  {"ingested": N, "rejected": [...]}
```

---

## Runtime data flow

```
ingest endpoint
      │  desc := pipeline.LookupType(typeName)   // atomic routing table load
      │  non-blocking send to desc.Queue
      ▼
 per-type queue  (chan CSVitem, capacity = writerQueueSize)
   = desc.Queue = worker.Queue
      │
      │  Worker[CSVitem] goroutine (pipeline.NewWorker)
      │  calls writer.Add(item)
      ▼
 writer buffer  ([]string in FileWriter / BlobWriter)
      │
      │  flush triggered by:
      │    • buffer len ≥ writerBatchSize  (threshold-based, inside Add)
      │    • ticker every 500 ms     (time-based, inside Start goroutine)
      │    • ctx.Done()              (shutdown flush, drains queue first)
      ▼
 file:  rootDir/{def.Folder}/{timestamp}_{typeName}.csv
 blob:  https://{account}.blob.core.windows.net/{def.Folder}/{timestamp}_{typeName}
```

---

## Concurrency model

| Component | Goroutines | Synchronisation |
|---|---|---|
| Worker | 1 per type (reads per-type queue) | none — single consumer |
| FileWriter / BlobWriter flush loop | 1 per type | `sync.Mutex` shared with `Add` |
| Routing table updates | atomic | `atomic.Pointer[RoutingTable]` copy-on-write |

---

## Graceful shutdown

```
cancel()          // signals ctx.Done() to all goroutines

wg.Wait()         // blocks until:
                  //   each Worker drains its queue and exits
                  //   each writer flush loop does a final flush and exits
```

Writers flush any remaining buffer items before returning, so no data is lost on clean shutdown.
