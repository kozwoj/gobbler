# Gobbler Self-Logging — Design Notes

## Purpose

A running Gobbler instance uses the Gobbler Client SDK to emit its own
operational telemetry to a second ("logger") Gobbler instance. This gives
operators a queryable, structured record of ingest throughput, flush activity,
and internal errors without adding an external observability dependency.

The design is sometimes called "Gobbler monitoring Gobbler".

---

## Item type definitions (what gets logged)

Four item types are defined on the logger Gobbler instance. The instrumenting
Gobbler server registers these type names with its embedded client at configure
time.

### `gobbler-ingest-event`

Emitted by `handleIngest` after each completed request. One item per POST.

| Field        | Type   | Description                                      |
|--------------|--------|--------------------------------------------------|
| `requestId`  | string | Random or trace ID for correlation               |
| `itemsIn`    | int    | Total items received in the batch                |
| `ingested`   | int    | Items accepted by the pipeline                   |
| `rejected`   | int    | Items in the rejected list                       |
| `statusCode` | int    | HTTP response status (200 or 400)                |
| `durationMs` | int    | Handler wall time in milliseconds                |

### `gobbler-writer-flush`

Emitted by `FileWriter` / `BlobWriter` after each successful flush. One item
per flush.

| Field         | Type   | Description                                      |
|---------------|--------|--------------------------------------------------|
| `typeName`    | string | Item type being flushed                          |
| `output`      | string | Active file path or blob name                    |
| `itemsFlushed`| int    | Number of CSV lines written in this flush        |
| `durationMs`  | int    | Flush wall time in milliseconds                  |

### `gobbler-writer-error`

Emitted wherever a writer currently has a `fmt.Println` on an error path
(blob append failures, file write failures, rotate errors). One item per error.

| Field       | Type   | Description                                       |
|-------------|--------|---------------------------------------------------|
| `typeName`  | string | Item type whose writer encountered the error      |
| `operation` | string | `"flush"`, `"rotate"`, or `"open"`               |
| `errorMsg`  | string | Error message string                              |

### `gobbler-pipeline-event`

Emitted by `handlePipelineStart`, `handlePipelineStop`, `handlePipelineRotate`.
Low-volume lifecycle events useful for auditing restarts.

| Field      | Type   | Description                                         |
|------------|--------|-----------------------------------------------------|
| `event`    | string | `"start"`, `"stop"`, or `"rotate"`                 |
| `typeName` | string | Relevant type name for `"rotate"`; empty otherwise  |

---

## Item definition JSON (register on the logger instance)

Post each definition to `POST /gobbler/definition/add` on the logger Gobbler
instance before starting it. The `folder` values follow Azure container naming
rules and can be changed to match your storage layout.

### `gobbler-ingest-event`

```json
{
  "name": "gobbler-ingest-event",
  "documentation": "One record per POST /gobbler/ingest request on the instrumented Gobbler instance.",
  "folder": "gobbler-ingest",
  "latencyMinutes": 5,
  "orderedColumns": [
    { "name": "requestId",  "type": "string",  "optional": true  },
    { "name": "itemsIn",    "type": "int",     "optional": false },
    { "name": "ingested",   "type": "int",     "optional": false },
    { "name": "rejected",   "type": "int",     "optional": false },
    { "name": "statusCode", "type": "int",     "optional": false },
    { "name": "durationMs", "type": "int",     "optional": false }
  ]
}
```

### `gobbler-writer-flush`

```json
{
  "name": "gobbler-writer-flush",
  "documentation": "One record per successful writer flush (FileWriter or BlobWriter).",
  "folder": "gobbler-writer",
  "latencyMinutes": 5,
  "orderedColumns": [
    { "name": "typeName",     "type": "string", "optional": false },
    { "name": "output",       "type": "string", "optional": true  },
    { "name": "itemsFlushed", "type": "int",    "optional": false },
    { "name": "durationMs",   "type": "int",    "optional": false }
  ]
}
```

### `gobbler-writer-error`

```json
{
  "name": "gobbler-writer-error",
  "documentation": "One record per writer error (flush, rotate, or open failure).",
  "folder": "gobbler-writer",
  "latencyMinutes": 5,
  "orderedColumns": [
    { "name": "typeName",  "type": "string", "optional": false },
    { "name": "operation", "type": "string", "optional": false },
    { "name": "errorMsg",  "type": "string", "optional": false }
  ]
}
```

### `gobbler-pipeline-event`

```json
{
  "name": "gobbler-pipeline-event",
  "documentation": "One record per pipeline lifecycle event (start, stop, rotate).",
  "folder": "gobbler-pipeline",
  "latencyMinutes": 5,
  "orderedColumns": [
    { "name": "event",    "type": "string", "optional": false },
    { "name": "typeName", "type": "string", "optional": true  }
  ]
}
```

---

## Where `client.Log(...)` calls go

| Call site                                   | Type logged                  |
|---------------------------------------------|------------------------------|
| `handleIngest` — end of handler             | `gobbler-ingest-event`       |
| `FileWriter.flush()` — on success           | `gobbler-writer-flush`       |
| `BlobWriter.flush()` — on success           | `gobbler-writer-flush`       |
| `FileWriter.flush()` — on error             | `gobbler-writer-error`       |
| `BlobWriter.flush()` — on error             | `gobbler-writer-error`       |
| `BlobWriter.rotate()` — on error            | `gobbler-writer-error`       |
| `handlePipelineStart` — on success          | `gobbler-pipeline-event`     |
| `handlePipelineStop` — on success           | `gobbler-pipeline-event`     |
| `handlePipelineRotate` — on success         | `gobbler-pipeline-event`     |

The logger client is a no-op (`gobblerclient.Nop()`) when logging is not
configured, so all call sites are safe unconditionally — no `if loggerEnabled`
guards needed anywhere.

---

## How to configure and start the logging client

### Chosen approach: extend `pipeline/configure`

Logger configuration is added as optional fields to the existing
`POST /gobbler/pipeline/configure` request body. No new endpoint is needed for
initial setup.

Adopting this approach also requires renaming `batchSize` → `writerBatchSize`
in the configure payload so it is unambiguous alongside `loggerBatchSize`.

```json
{
  "mode": "blob",
  "accountName": "...",
  "accountKey": "...",
  "workerQueueSize": 100,
  "writerBatchSize": 50,
  "loggerEndpoint": "http://logger-gobbler:8080",
  "loggerTypes": [
    "gobbler-ingest-event",
    "gobbler-writer-flush",
    "gobbler-writer-error",
    "gobbler-pipeline-event"
  ],
  "loggerBatchSize": 20,
  "loggerFlushInterval": "10s"
}
```

If `loggerEndpoint` is absent or empty, logging is disabled and a no-op client
is used.

**Rationale for tying logging to the pipeline configure/start/stop lifecycle:**

- The logger is only meaningful while the pipeline is running; there is nothing
  to log when the pipeline is stopped.
- Reuses the existing operator workflow: one configure call, one start call.
- Validation of the logger target (running + types present) happens at configure
  time, before any pipeline activity begins.
- The client is `Close()`d automatically when `pipeline/stop` is called.

**Logger server swap requires a dedicated endpoint.**
`pipeline/configure` can set the initial logger target, but hot-swapping to a
new logger server while the pipeline is running (e.g. planned maintenance on the
logger instance) needs its own route, analogous to `SwapServer` in the client
SDK. Proposed: `POST /gobbler/pipeline/swap-logger` with body
`{"loggerEndpoint": "http://new-logger:8080"}`. The handler validates the new
target (running + types present), then calls the embedded client's `SwapServer`.
If validation fails the current logger is kept unchanged.

### Alternative: separate `/gobbler/logger` route group (rejected)

A mirror of the pipeline routes (`configure`, `start`, `stop`, `status`) was
considered. Rejected because it introduces a second lifecycle the operator must
manage in parallel and creates awkward states (logging started, pipeline
stopped).

### Alternative: logger config in `pipeline/start` body (rejected)

Rejected because `start` currently takes no body (adding one is a usage change)
and validation would be deferred until start time rather than configure time.

---

## Logger client lifecycle inside the server

```
pipeline/configure  →  gobblerclient.New(loggerEndpoint, WithTypes(...))
                        validates: running=true AND all types present
                        stores client on Server struct (or nopClient if absent/failed)

pipeline/start      →  logger client flush goroutine already running
                        (started inside gobblerclient.New)

pipeline/stop       →  logger.Close()  — flushes buffer, stops goroutine
                        replaces stored client with nopClient

pipeline/configure  →  if reconfigured while running, Close old client first,
  (again)              then construct new one
```

---

## Setup script (planned)

A PowerShell script (`tester/setup-logger.ps1` or similar) will automate the
complete bootstrap of the logger Gobbler instance:

1. POST each of the four item definitions to the logger's `/gobbler/definition/add`
2. POST `/gobbler/pipeline/configure` with the desired storage mode and credentials
3. POST `/gobbler/pipeline/start`
4. GET `/gobbler/pipeline/status` to confirm `running: true`

The script will read connection details (endpoint URL, storage account, key) from
a parameter file or environment variables so it is safe to commit without secrets.

---

## Open questions

- Should logging to self (same Gobbler instance) be supported? Risk: a write
  error causes a log call which causes a write which causes a log call...
  Recommend: disallow self-logging by comparing the logger endpoint host+port
  against the server's own listen address at configure time.
- Should `loggerTypes` default to all four types if `loggerEndpoint` is set
  but `loggerTypes` is omitted?
- Should logger failures (can't reach the logger Gobbler) be surfaced anywhere
  (e.g. in `pipeline/status`)? Or silently swallowed? Surfacing them in status
  connects back to the diagnostic events design (see `gobbler-client.md` open
  questions).
