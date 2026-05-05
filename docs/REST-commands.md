# Gobbler REST Command Reference

This document is the authoritative reference for all Gobbler REST API endpoints. It is derived directly from the server implementation. Update this document whenever a route is added, removed, or its input/output shape changes.

All endpoints are prefixed with `/gobbler`. All requests and responses use `Content-Type: application/json`.

---

## Discovery endpoints

Every route group and every command path supports a trailing-slash GET that returns a human-readable description of the route or group. These are not listed individually below; the pattern is:

| Request | Returns |
|---|---|
| `GET /gobbler/` | list of route groups |
| `GET /gobbler/definition/` | list of definition commands |
| `GET /gobbler/pipeline/` | list of pipeline commands |
| `GET /gobbler/ingest/` | description of the ingest command |
| `GET /gobbler/<command>/` | description of that specific command |

---

## definition group

### `POST /gobbler/definition/add`

Parses, validates, and registers a new item type definition. If the pipeline is already running, also creates and starts the writer and worker for the new type immediately.

**Input** — Item definition JSON object (schema in [`docs/item_schema.json`](item_schema.json)):

```json
{
  "name": "alpha",
  "documentation": "optional free-text description",
  "folder": "optional-subfolder-or-container-name",
  "latencyMinutes": 5,
  "orderedColumns": [
    { "name": "userId",    "type": "string" },
    { "name": "score",     "type": "real",   "optional": true, "defaultValue": 0.0 },
    { "name": "active",    "type": "bool" },
    { "name": "createdAt", "type": "datetime" },
    { "name": "payload",   "type": "dynamic" },
    { "name": "elapsed",   "type": "timespan" },
    { "name": "count",     "type": "int" }
  ]
}
```

Field notes:
- `name` — required; used as the type identifier in ingest calls. Must be a valid file/directory name (no path separators etc.). The reserved name `timestamp` is rejected.
- `documentation` — optional string; ignored by the pipeline.
- `folder` — optional; names the output subdirectory (file mode) or container (blob mode). Defaults to the value of `name` when omitted.
- `latencyMinutes` — optional non-negative integer; defaults to `1`.
- `orderedColumns` — required, non-empty array. Column order determines CSV column order. Column `name` `"timestamp"` is reserved. Supported `type` values: `"bool"`, `"datetime"`, `"dynamic"`, `"int"`, `"real"`, `"string"`, `"timespan"`.

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Definition accepted |
| 400 | `{"error": "..."}` | Malformed JSON or validation failure |
| 409 | `{"error": "..."}` | A definition with that name is already registered |
| 500 | `{"error": "..."}` | Pipeline is running but writer/worker startup failed (definition is rolled back) |

---

### `GET /gobbler/definition/list`

Returns all currently registered item type definitions as a JSON array of full definition objects.

**Input:** none

**Responses:**

| Status | Body |
|---|---|
| 200 | `[{ "TypeName": "alpha", "Documentation": "...", "Folder": "alpha", "Latency": 1, "Columns": [...] }, ...]` |

Returns an empty array `[]` if no definitions are registered.

---

### `GET /gobbler/definition/names`

Returns only the registered type-name strings. Lighter-weight than `/list`; useful for clients that only need to check which types exist.

**Input:** none

**Responses:**

| Status | Body |
|---|---|
| 200 | `["alpha", "beta", ...]` |

Returns an empty array `[]` if no definitions are registered. Order is unspecified (Go map iteration).

---

### `POST /gobbler/definition/remove`

Removes an item type definition. If the pipeline is running, stops the type's writer (flushing and closing the current output), removes the type from the routing table, and then removes the definition.

**Input:**

```json
{ "typeName": "alpha" }
```

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Definition removed |
| 400 | `{"error": "typeName is required"}` | Missing or empty `typeName` field |
| 404 | `{"error": "..."}` | Type name not found |

---

## pipeline group

### `POST /gobbler/pipeline/configure`

Sets the pipeline's storage mode, storage credentials/path, queue sizes, and writer batch size. Must be called before `pipeline/start`. Cannot be called while the pipeline is running; stop it first.

**Input:**

```json
{
  "mode":            "file",
  "outputDir":       "/path/to/output",
  "accountName":     "",
  "accountKey":      "",
  "writerQueueSize": 100,
  "writerBatchSize": 50,
  "loggerEndpoint":      "http://collector:8080",
  "loggerTypes":         ["gobbler.ingest", "gobbler.pipeline"],
  "loggerBatchSize":     100,
  "loggerFlushInterval": "30s"
}
```

Field rules:
- `mode` — required; `"file"` or `"blob"`.
- `outputDir` — required when `mode` is `"file"`.
- `accountName`, `accountKey` — both required when `mode` is `"blob"`.
- `writerQueueSize` — capacity of each per-type writer's internal channel.
- `writerBatchSize` — number of CSV rows accumulated before the writer flushes to storage.
- `loggerEndpoint` — optional; URL of a separate Gobbler server that will receive this server's own operational events. When omitted, self-logging is disabled.
- `loggerTypes` — optional array of item type name strings the self-logging client will emit. Only meaningful when `loggerEndpoint` is set. Standard types (defined in [`docs/gobbler-logging.md`](gobbler-logging.md)): `"gobbler-ingest-event"`, `"gobbler-writer-flush"`, `"gobbler-writer-error"`, `"gobbler-pipeline-event"`. These types must already be registered on the target logger Gobbler instance.
- `loggerBatchSize` — optional; batch size for the self-logging client. Defaults to `100` when omitted or `0`.
- `loggerFlushInterval` — optional; Go duration string (e.g. `"30s"`) for the self-logging client flush interval. Defaults to `10s` when omitted.

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Configuration accepted |
| 400 | `{"error": "..."}` | Invalid JSON, unknown mode, or missing required field for the chosen mode |
| 409 | `{"error": "cannot reconfigure while pipeline is running; stop it first"}` | Pipeline is currently running |

---

### `POST /gobbler/pipeline/start`

Starts the pipeline. Creates the central dispatcher, routing table, workers, and writers for all currently registered definitions. Requires `pipeline/configure` to have been called and at least one definition to be registered.

**Input:** none (body ignored)

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Pipeline started |
| 409 | `{"error": "pipeline not configured; call pipeline/configure first"}` | Not yet configured |
| 409 | `{"error": "pipeline is already running"}` | Already running |
| 409 | `{"error": "no item type definitions registered; call definition/add first"}` | No definitions |
| 500 | `{"error": "..."}` | Writer/worker startup failed; all partially started goroutines are rolled back |

---

### `POST /gobbler/pipeline/stop`

Stops the pipeline. Cancels all goroutines, causes every writer to flush its buffer and close its current output file/blob, then resets routing state. The pipeline can be reconfigured and restarted after this call.

**Input:** none (body ignored)

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Pipeline stopped |
| 409 | `{"error": "pipeline is not running"}` | Not currently running |

---

### `POST /gobbler/pipeline/rotate`

Tells a specific type's writer to flush its buffer, close the current output file/blob, and open a new one. Used to make a completed file/blob accessible without stopping the whole pipeline.

**Input:**

```json
{ "typeName": "alpha" }
```

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | `{"status": "ok"}` | Rotation triggered |
| 400 | `{"error": "typeName is required"}` | Missing or empty `typeName` |
| 404 | `{"error": "unknown type: alpha"}` | Type not registered or not running |
| 409 | `{"error": "pipeline is not running"}` | Pipeline not running |

---

### `GET /gobbler/pipeline/status`

Returns the current status and statistics of the pipeline.

**Input:** none

**Response — pipeline not configured:**

```json
{
  "configured": false,
  "running": false,
  "registeredDefinitions": 0
}
```

**Response — configured but not running (no logger endpoint set):**

```json
{
  "configured": true,
  "running": false,
  "registeredDefinitions": 2,
  "mode": "file",
  "writerQueueSize": 100,
  "writerBatchSize": 50,
  "logger": { "configured": false }
}
```

**Response — configured but not running (logger endpoint set):**

```json
{
  "configured": true,
  "running": false,
  "registeredDefinitions": 2,
  "mode": "file",
  "writerQueueSize": 100,
  "writerBatchSize": 50,
  "logger": { "configured": true }
}
```

**Response — running, logger started successfully:**

```json
{
  "configured": true,
  "running": true,
  "registeredDefinitions": 2,
  "mode": "file",
  "writerQueueSize": 100,
  "writerBatchSize": 50,
  "logger": { "configured": true, "running": true },
  "writers": {
    "alpha": {
      "itemsInBuffer": 12,
      "itemsWritten":  4800,
      "lastFlush":     "2026-05-03T14:22:01Z",
      "currentOutput": "/output/alpha/alpha-2026-05-03.csv"
    },
    "beta": {
      "itemsInBuffer": 0,
      "itemsWritten":  900,
      "lastFlush":     "2026-05-03T14:20:55Z",
      "currentOutput": "/output/beta/beta-2026-05-03.csv"
    }
  }
}
```

**Response — running, logger failed to start (pipeline started anyway):**

```json
{
  "configured": true,
  "running": true,
  "registeredDefinitions": 2,
  "mode": "file",
  "writerQueueSize": 100,
  "writerBatchSize": 50,
  "logger": { "configured": true, "running": false, "error": "gobblerclient: server not running at http://logger:8080" },
  "writers": { ... }
}
```

Always present fields: `configured` (bool), `running` (bool), `registeredDefinitions` (int).
Present when configured: `mode`, `writerQueueSize`, `writerBatchSize`, `logger` object.
`logger` fields: `configured` (bool — whether `loggerEndpoint` was set in the last configure call); `running` (bool — present when pipeline is running, true if the client started successfully); `error` (string — present when pipeline is running and logger failed to start, persists until `pipeline/stop`).
Present when running: `writers` map keyed by type name, each with `itemsInBuffer`, `itemsWritten`, `lastFlush`, `currentOutput`.

---

## ingest group

### `POST /gobbler/ingest`

Ingests an array of typed items into the running pipeline. This is the primary function of Gobbler.

**Input** — JSON array of objects, each object having exactly one key (the type name) whose value is the field map for that item:

```json
[
  { "alpha": { "userId": "u123", "score": 9.5, "active": true, "createdAt": "2026-05-03 14:00:00.000", "payload": {"k": 1}, "elapsed": "1h30m", "count": 42 } },
  { "beta":  { "label": "x", "value": 7 } }
]
```

**Responses:**

| Status | Body | Condition |
|---|---|---|
| 200 | see below | Items processed (some may be rejected) |
| 400 | `{"error": "..."}` | Body is not a valid JSON array, array is empty, or every element failed to parse |
| 409 | `{"error": "pipeline is not running"}` | Pipeline not started |

**200 response body:**

```json
{
  "ingested": 1,
  "rejected": [
    { "error": "..." },
    { "typeName": "beta", "errors": ["field 'value': wrong type"] },
    { "typeName": "gamma", "error": "type not registered" },
    { "typeName": "alpha", "error": "worker queue full" }
  ]
}
```

- `ingested` — count of items successfully routed to a writer queue.
- `rejected` — array of rejection records; `null` when all items were accepted. Each record is one of:
  - `{"error": "..."}` — item-level JSON parse failure (no type name recoverable).
  - `{"typeName": "...", "errors": [...]}` — field conversion errors.
  - `{"typeName": "...", "error": "type not registered"}` — type name unknown to the routing table.
  - `{"typeName": "...", "error": "worker queue full"}` — writer's queue is at capacity; item dropped.

---

## Error response shape

All error responses share the same shape regardless of status code:

```json
{ "error": "human-readable message" }
```

---

## Typical startup sequence

```
POST /gobbler/pipeline/configure   { "mode": "file", "outputDir": "/data", "writerQueueSize": 100, "writerBatchSize": 50 }
POST /gobbler/definition/add       { "name": "alpha", "orderedColumns": [...] }
POST /gobbler/definition/add       { "name": "beta",  "orderedColumns": [...] }
POST /gobbler/pipeline/start
POST /gobbler/ingest               [{"alpha": {...}}, {"beta": {...}}]
GET  /gobbler/pipeline/status
POST /gobbler/pipeline/stop
```
