# Server–Pipeline–Writer Wiring

This note describes how the REST server, pipeline dispatcher, per-type workers, and writers are connected at startup and at runtime.

---

## Startup sequence

```
main()
 │
 ├─ pipeline.Start(ctx, wg, centralQueueSize)   // central input queue + dispatcher goroutine
 │
 └─ httpServer.ListenAndServe(...)              // REST endpoints ready
```

The central queue and dispatcher are started once. No item types are registered yet; the ingest endpoint will reject all types until definitions are added.

---

## Registering a new item type (definition endpoint)

The REST definition endpoint performs these steps in order:

```
1. items.CreateItemDefinition(body)          // parse + validate JSON schema → ItemDefinition

2. items.DefinitionList.AddDefinition(def)   // store in the items registry

3. writer := writers.NewFileWriter(rootDir, def, batchSize)   // file writer  ─┐ pick one
        or writers.NewBlobWriter(blobCfg, def, batchSize)     // blob writer  ─┘

4. writer.Start(ctx, wg)                     // start time-based flush goroutine

5. worker := pipeline.NewWorker(ctx, wg, perTypeQueueSize, writer.Add)
   // generic Worker[CSVitem]; its goroutine calls writer.Add for every dequeued item

6. desc := &pipeline.TypeDescriptor{
       Definition: def,
       Queue:      worker.Queue,   // send-only channel exposed by the worker
       Config:     writerConfig,   // WriterKindFile or WriterKindBlob + BlobConfig
   }

7. pipeline.AddItemType(pipeline.ItemType(def.TypeName), desc)
   // atomic pointer swap → dispatcher and ingest endpoint now recognise this type
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
 │   ├─ desc := pipeline.LookupType(typeName)     // nil → 400 unknown type
 │   ├─ csv, errs := items.ConvertItem(item, definitions, timestamp)
 │   │                                             // validate + convert to CSV string
 │   └─ pipeline.Enqueue(pipeline.CSVitem{         // false → 503 overloaded
 │          Type: typeName, CSV: csv})
 │
 └─ 200 OK (with per-item error details for any rejected items)
```

---

## Runtime data flow

```
ingest endpoint
      │  pipeline.Enqueue(CSVitem)
      ▼
 central input queue  (chan CSVitem, capacity = centralQueueSize)
      │
      │  dispatcher goroutine (pipeline.Start)
      │  table := routing.Load()
      │  desc  := (*table)[item.Type]
      ▼
 per-type queue  (chan CSVitem, capacity = perTypeQueueSize)
   = desc.Queue = worker.Queue
      │
      │  Worker[CSVitem] goroutine (pipeline.NewWorker)
      │  calls writer.Add(item)
      ▼
 writer buffer  ([]string in FileWriter / BlobWriter)
      │
      │  flush triggered by:
      │    • buffer len ≥ batchSize  (threshold-based, inside Add)
      │    • ticker every 500 ms     (time-based, inside Start goroutine)
      │    • ctx.Done()              (shutdown flush)
      ▼
 file:  rootDir/{def.Folder}/{timestamp}_{typeName}.csv
 blob:  https://{account}.blob.core.windows.net/{def.Folder}/{timestamp}_{typeName}
```

---

## Concurrency model

| Component | Goroutines | Synchronisation |
|---|---|---|
| Dispatcher | 1 (reads central queue) | none — atomic routing table load |
| Worker | 1 per type (reads per-type queue) | none — single consumer |
| FileWriter / BlobWriter flush loop | 1 per type | `sync.Mutex` shared with `Add` |
| Routing table updates | atomic | `atomic.Pointer[RoutingTable]` copy-on-write |

---

## Graceful shutdown

```
cancel()          // signals ctx.Done() to all goroutines

wg.Wait()         // blocks until:
                  //   dispatcher exits
                  //   each Worker exits
                  //   each writer flush loop does a final flush and exits
```

Writers flush any remaining buffer items before returning, so no data is lost on clean shutdown.
