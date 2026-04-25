# Implementation Notes — REST Route Handler Wiring

This note describes the changes made to wire up the previously stubbed REST route handlers.

---

## Changes by file

### `pipeline/dispatcher.go` — new `RemoveItemType` function

Added a counterpart to the existing `AddItemType`:

```go
func RemoveItemType(t ItemType)
```

Builds a new `RoutingTable` that omits the given type and atomically swaps it in via `atomic.Pointer`, exactly as `AddItemType` does. This guarantees the dispatcher goroutine never observes a partially-updated table.

---

### `items/definition_list.go` — new `RemoveDefinition` method

Added to `DefinitionList`:

```go
func (dl DefinitionList) RemoveDefinition(typeName string) error
```

Looks up the type name, returns `ErrDefinitionNotFound` if absent, otherwise deletes the entry from the map and returns `nil`.

---

### `server/server.go` — new field and new helper

**New field on `Server`:**

```go
pipelineCtx context.Context
```

Stored alongside the existing `cancel` field. Needed so that `startType` (see below) can derive per-type child contexts from the pipeline root context.

**New `writers` import** added to the server package.

**New `startType` helper method:**

```go
func (s *Server) startType(def items.ItemDefinition) error
```

Encapsulates the full wiring sequence for a single item type as documented in `docs/wiring.md`:

1. Derives a child context (`context.WithCancel(s.pipelineCtx)`) so each type can be cancelled independently.
2. Constructs either a `writers.FileWriter` or a `writers.BlobWriter` based on `s.config.Mode`.
3. Calls `writer.Start(ctx, &entry.wg)` to launch the time-based flush goroutine.
4. Calls `pipeline.NewWorker[pipeline.CSVitem](ctx, &entry.wg, workerQueueSize, writer.Add)` to launch the per-type dispatch goroutine.
5. Stores the `typeEntry` (writer + cancel + wg) in `s.types`.
6. Calls `pipeline.AddItemType(...)` to make the type visible to the dispatcher.

Must be called with `s.mu` write-locked and only while `s.running` is true (i.e., `s.pipelineCtx` is set). Shared by both `handleDefinitionAdd` (hot-add while running) and `handlePipelineStart`.

---

### `server/definition_routes.go` — three handlers implemented

**`handleDefinitionAdd` (POST `/gobbler/definition/add`)**

Input: item definition JSON object (see `docs/item_schema.json`), e.g.:
```json
{
  "name": "vmShutdown",
  "documentation": "VM shutdown event",
  "folder": "vm_events",
  "latency": 5,
  "orderedColumns": [
    {"name": "vmId",   "type": "string"},
    {"name": "reason", "type": "string", "optional": true}
  ]
}
```

1. Reads and parses the request body as an item definition JSON object via `items.CreateItemDefinition`.
2. Acquires the write lock.
3. Calls `s.definitions.AddDefinition(def)` — returns 409 Conflict if the type name is already registered.
4. If the pipeline is running, calls `s.startType(def)`. On failure, rolls back the definition registration before returning 500.
5. Returns `{"status": "ok"}`.

**`handleDefinitionList` (GET `/gobbler/definition/list`)**

1. Acquires the read lock.
2. Copies `s.definitions` into a slice of `items.ItemDefinition`.
3. Returns the slice as a JSON array.

**`handleDefinitionRemove` (POST `/gobbler/definition/remove`)**

Input: `{"typeName": "string"}`

1. Decodes request body.
2. Acquires the write lock; returns 404 if the type name is not found.
3. If the pipeline is running: atomically removes the type from the routing table (`pipeline.RemoveItemType`), removes it from `s.types`, and captures the `typeEntry`.
4. Removes the definition from `s.definitions`.
5. Releases the lock.
6. If there was a running entry, calls `entry.cancel()` then `entry.wg.Wait()` **outside the lock** to avoid deadlock during writer shutdown.
7. Returns `{"status": "ok"}`.

---

### `server/pipeline_routes.go` — five handlers implemented

**`handlePipelineConfigure` (POST `/gobbler/pipeline/configure`)**

Input (file mode):
```json
{"mode": "file", "outputDir": "/data/gobbler", "centralQueueSize": 1000, "workerQueueSize": 200, "batchSize": 50}
```
Input (blob mode):
```json
{"mode": "blob", "accountName": "myaccount", "accountKey": "base64key==", "centralQueueSize": 1000, "workerQueueSize": 200, "batchSize": 50}
```

1. Decodes JSON into a local struct.
2. Validates: mode must be `"file"` or `"blob"`; file mode requires `outputDir`; blob mode requires both `accountName` and `accountKey`.
3. Returns 409 if the pipeline is already running.
4. Stores a `*pipeline.Config` on the server.

**`handlePipelineStart` (POST `/gobbler/pipeline/start`)**

Pre-conditions checked (each returns 409):
- Config must have been set.
- Pipeline must not already be running.
- At least one definition must be registered.

Sequence:
1. Creates a root `context.WithCancel(context.Background())` and stores it on the server.
2. Calls `pipeline.Start(ctx, &s.wg, centralQueueSize)` to start the dispatcher.
3. Calls `s.startType(def)` for every registered definition.
4. On any failure: cancels the root context, waits for all goroutines, calls `pipeline.Reset()`, clears `s.types` and the context fields, returns 500.
5. Sets `s.running = true` and returns `{"status": "ok"}`.

**`handlePipelineStop` (POST `/gobbler/pipeline/stop`)**

1. Returns 409 if not running.
2. Sets `s.running = false`, captures and clears `cancel`, `pipelineCtx`, and `s.types`.
3. Releases the lock.
4. Calls `cancel()`, waits on `s.wg` (dispatcher) and each `entry.wg` (per-type goroutines), then calls `pipeline.Reset()`.
5. Returns `{"status": "ok"}`.

**`handlePipelineRotate` (POST `/gobbler/pipeline/rotate`)**

Input: `{"typeName": "string"}`

1. Returns 409 if not running; 404 if the type is not in `s.types`.
2. Calls `entry.writer.Rotate()` under the read lock.
3. Returns `{"status": "ok"}`.

**`handlePipelineStatus` (GET `/gobbler/pipeline/status`)**

Returns a JSON object containing:
- `configured` (bool)
- `running` (bool)
- `registeredDefinitions` (int)
- When configured: `mode`, `centralQueueSize`, `workerQueueSize`, `batchSize`
- When running: `activeTypes` (array of type name strings)

---

### `server/ingest_routes.go` — handler implemented

**`handleIngest` (POST `/gobbler/ingest`)**

Input: JSON array of `{"typeName": {...fields}}` objects, e.g.:
```json
[
  {"vmShutdown": {"vmId": "vm-001", "reason": "OS update"}},
  {"vmReboot":   {"vmId": "vm-002", "eventTime": "2026-04-23 10:00:00.000"}}
]
```

1. Reads body; returns 409 if pipeline is not running.
2. Acquires the read lock only long enough to snapshot `s.definitions` into a local copy; releases before any conversion work.
3. Calls `items.SplitInput(body)` to parse the JSON array into `[]InputItem`; parse errors are collected into the `rejected` list.
4. Stamps the current UTC time as the `timestamp` string (`2006-01-02 15:04:05.000`).
5. For each `InputItem`:
   - Calls `items.ConvertItem(item, defsCopy, timestamp)` to validate and produce a CSV string. Validation errors are collected into `rejected`.
   - Calls `pipeline.Enqueue(CSVitem{...})`; if the central queue is full (`false` return), the item is added to `rejected` with `"error": "pipeline queue full"`.
   - On success, increments `ingested`.
6. Returns `{"ingested": N, "rejected": [...]}`.

---

## Locking strategy

The server uses `sync.RWMutex` (`s.mu`):

- **Read lock** for `status`, `rotate`, and the read-only snapshot inside `ingest`.
- **Write lock** for `configure`, `start`, `stop`, `definition/add`, and `definition/remove`.
- Goroutine shutdown (`cancel` + `wg.Wait`) is always done **after releasing the lock** to prevent the lock being held while blocked on a goroutine that might itself try to acquire it.
