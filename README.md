# Gobbler Overview

**Gobbler** is a configurable telemetry ingestion pipeline server written in Go. There are two aspects of Gobbler's configurability 
1. Before staring the pipeline the user can set up server's operational parameters like target storage (files vs. Azure blobs), ingestion queue sizes, batch sizes, and operational logging 
   
2. Before starting the pipeline, but also when it is running, the user can add definitions of the structure of telemetry items to be ingested and stored by Gobbler. 

Gobbler ingests and stores only telemetry items that have been defined and added to its item dictionary. Item definitions are JSON objects with the schema provided in `docs\JSON-schemas.md`. Below is an example of a VM shutdown event log item definition. 

``` JSON
{
  "name": "vm-shutdown",
  "documentation": "virtual machine shutdown event",
  "folder": "administration",
  "m": 10,
  "orderedColumns": 
      [
        { "name": "vmId", "type": "string" },
        { "name": "shutdownStart", "type": "datetime" },
        { "name": "shutdownReason", "type": "string", "defaultValue": "unknown", "optional": false }
      ]
}
```
Item definitions are added to Gobbler using the REST endpoint `POST /gobbler/definition/add`. 

Gobbler accepts arrays of JSON objects representing defined items types and 
- validates them
- converts well-formed items to CSV strings following the sequence of fields in item definition
- groups and buffers items of the same type
- appends the buffers to timestamped files/blobs named after item type names, and 
- rotates the files/blobs on the time interval set in item (`latencyMinutes` property above).

Files/blobs with items of the same type are stored in one directory/container, named after item type name or by the `folder` definition property. File/blob names have the following structure `YYYY-MM-DD_HH-MM-SS.mmm_<typeName>.csv`, where the file timestamp preceding the type name is equal to the timestamp of the first item stored in this file/blob. 

This convention makes it convenient for processing the CSV files with analytical DB systems like Kusto (aka Azure Analytics) or DockDB.

## Gobbler Architecture

Gobble architecture has four distinct parts
- the item definition part (`items` module)
- the ingestion pipeline (`pipeline` module) 
- the writers (`writers` module), and 
- the REST interface (`server` module)

### Item definitions

Item definition (description of its type) is given to Gobbler as a JSON object of the following schema

``` JSON
{
  "title": "itemTypeSchema",
  "description": "item (log entry) type description",
  "type": "object",
  "properties": {
    "name": {"type": "string"},
    "documentation": {"type": "string", "optional": true},
    "folder": {"type": "string", "minLength": 3, "optional": true},
    "latencyMinutes": {"type": "integer", "optional": true},
    "orderedColumns": {
      "type": "array",
      "items": {  
        "type": "object",
        "properties": {
          "name": { "type": "string"},  
          "type": { "type": "string"},
          "optional": { "type": "boolean", "optional": true},
          "defaultValue": { "type": ["string", "number", "boolean"], "optional": true}
        }
      },
      "uniqueItems": true,
      "minItems": 1
    }
  }
}
```

where 
- `name` is the item type name
- `folder` is the name of the file directory or blob container, in which files/blobs with ingested items of the named type will be stored
- `latencyMinutes` is the time where items file, or blob, will be rotated
- `columns` is order list of item fields, including information about field name, type, optionality, and default value. 

Fields can be of the following scalar types 
- bool
- datetime
- dynamic
- int
- real
- string, and 
- timespan.
  
A field can be declared as optional or required, and can have a default value. The interaction between these two properties is as follows
- if a field is required AND has a default value, then if the field is not assigned a value on input, the default value is used
- if a filed is optional, the default value is ignored even provided.

The value of `dynamic` fields should be a well-formed JSON object, which is used by Kusto for passing complex values. GOBBLER does not understand the structure of dynamic fields, but it validates that they contain well-formed JSON objects. 

In addition to the user-defined fields Gobbler adds, as the first field, a time stamps. The field is called `timestamp` and contains datetime of when the item was validated and converted to SCV string. This is important when passing CSV strings to analytical DB systems, and therefore the name `timestamp` is reserved (cannot be used in item definitions).  

While a log file/blog is opened for receiving new items it should not be used for analysis, like opening it in a spreadsheet or uploading it to Kusto. The property `latencyMinutes` of item definition declares after what time, since creation of the current file/blob, Gobbler should rotate it (close the current one and open a new one with a new time stamp). Gobbler provides a function to force rotation of a current file/blob if it is needed for analysis sooner than its latency would imply.      

### The ingestion pipeline

The architecture of the Gobbler's pipeline is described in more details in `docs\pipeline_architecture.md` document. Items are submitted to Gobbler as arrays of JSON object, and are processed in the following stages:

**Item Parsing & Validation**
  - Objects inconsistent with their definitions are rejected immediately and reported back to the caller
  - Valid objects/items are passed to the next stage

**Conversion to CSV**
  - Produces a compact, normalized representation of items as CSV strings, with fields in the order defined by the item type
  - The CSV string is wrapped in a `CSVitem` struct and sent directly to the appropriate per‑type worker queue
  - If the worker queue is full the item is rejected immediately and reported back to the caller — no silent drops

**Per‑Type Worker-Writer**
- Each item type has worker with a queue handling items of that type. A worker is composed of a batcher followed by a writer.
  - Batcher accumulated CSV strings in a buffer
  - When the buffer reaches its defined size, it is passed to the Writer
  - Writer appended the CSV items to a file/blob in the corresponding directory/container
  - When a file/blob reaches a configured size or time limit, the writer rotates it
- This is the end of the ingestion pipeline.

## The REST Interface

Gobbler exposes the following REST endpoints under the `/gobbler` prefix (default port 8080):  

**Definition management**
- `GET  /gobbler/definition/list` — list all registered item type definitions (full definition objects)
- `GET  /gobbler/definition/names` — list registered item type names only (lightweight `["name1", "name2", ...]`)
- `POST /gobbler/definition/add` — register a new item type (body: item definition JSON object)
- `POST /gobbler/definition/remove` — remove a registered item type (body: `{"typeName": "..."}`)

**Pipeline management**
- `POST /gobbler/pipeline/configure` — set storage mode and pipeline parameters
- `POST /gobbler/pipeline/start` — start the pipeline (requires at least one definition)
- `POST /gobbler/pipeline/stop` — stop the pipeline and flush all writers
- `POST /gobbler/pipeline/rotate` — force rotation of a type's current file/blob (body: `{"typeName": "..."}`)
- `GET  /gobbler/pipeline/status` — return pipeline configuration and running state

**Ingestion**
- `POST /gobbler/ingest` — ingest an array of typed JSON items (body: `[{"typeName": {...fields}}, ...]`); returns 400 if the body is not a valid JSON array or contains no parseable items, 200 with `{"ingested": N, "rejected": [...]}` otherwise

More detailed description of the REST interfaces is provides in `docs\REST-commands.md` document.

## Gobbler Logging

Gobbler can log its own operational events, using Gobbler Client SDK, to a second Gobbler instance (the "logger Gobbler"), enabling structured, queryable telemetry about ingestion performance and writer behaviour. This self-logging feature is configured via the `POST /gobbler/pipeline/configure` endpoint using the following fields:

| Field | Type | Description |
|---|---|---|
| `loggerEndpoint` | string | URL of the receiving Gobbler instance, e.g. `http://host:8081` |
| `loggerTypes` | array of strings | Item type names the instrumented instance will emit |
| `loggerBatchSize` | int | Client batch size; 0 uses the default (100) |
| `loggerFlushInterval` | string | Go duration string, e.g. `"30s"`; `""` uses the default (10s) |

When `loggerEndpoint` is set, the server constructs an internal [`gobbler-client`](https://github.com/kozwoj/gobbler-client) that ships operational events to the logger instance. The client buffers items in memory and flushes them in batches; if the logger is unreachable it returns `ErrBufferFullServerDown` so the instrumented instance continues operating without blocking. Four event types are defined:

| Type name | Folder | Description |
|---|---|---|
| `gobbler-ingest-event` | `gobbler-ingest` | One record per `POST /gobbler/ingest` call: request ID, items in/ingested/rejected, status code, duration |
| `gobbler-writer-flush` | `gobbler-writer` | One record per successful writer flush: item type, output path, items flushed |
| `gobbler-writer-error` | `gobbler-writer` | One record per writer error: item type, operation, error message |
| `gobbler-pipeline-event` | `gobbler-pipeline` | One record per pipeline lifecycle event (start, stop, rotate) |

The logger Gobbler instance must be started and configured independently before the instrumented instance is started. The helper script `scripts\setup-logger.ps1` automates this: it registers the four definitions above, configures the logger in file mode, and starts its pipeline.

```powershell
# Windows — start logger instance first (separate terminal)
.\gobbler.exe --port 8081

# Then run the setup script (the -OutputDir parameter is required)
.\scripts\setup-logger.ps1 -OutputDir C:\temp\gobbler-logs

# Linux / macOS equivalent
./gobbler --port 8081
./scripts/setup-logger.ps1 -OutputDir /tmp/gobbler-logs
```

The script accepts optional parameters `-LoggerUrl` (default `http://localhost:8081`), `-BatchSize` (default 50), and `-QueueSize` (default 100). Remote setup is described in `scripts\setup-logger-remotely.md`.

## Quick Start

This section walks through building Gobbler, starting it, configuring the pipeline, and sending test data using the tools in the `tester/` directory.

### 1. Build

```powershell
# From the repository root
go build -o gobbler.exe .        # Windows
go build -o gobbler      .       # Linux / macOS
```

### 2. Start the server

```powershell
./gobbler.exe                    # listens on :8080 (default)
./gobbler.exe --port 9090        # custom port
```

### 3. Configure the pipeline

Use the REST endpoint or any HTTP client. An example using `curl`:

```bash
# File-mode output
curl -X POST http://localhost:8080/gobbler/pipeline/configure \
     -H "Content-Type: application/json" \
     -d '{"mode":"file","outputDir":"/tmp/gobbler-out","writerBatchSize":50,"writerQueueSize":100}'

# Azure Blob mode
curl -X POST http://localhost:8080/gobbler/pipeline/configure \
     -H "Content-Type: application/json" \
     -d '{"mode":"blob","accountName":"<account>","accountKey":"<key>","writerBatchSize":50,"writerQueueSize":100}'
```

Ready-to-run HTTP requests are also available in [tester/docs/gobbler_REST.http](tester/docs/gobbler_REST.http).

### 4. Register item type definitions

Add at least one definition before starting the pipeline:

```bash
curl -X POST http://localhost:8080/gobbler/definition/add \
     -H "Content-Type: application/json" \
     -d '{"name":"alpha","orderedColumns":[{"name":"label","type":"string"},{"name":"value","type":"int"},{"name":"ts","type":"datetime"}]}'
```

Example definitions for all built-in test types (`alpha`, `beta`, `gamma`, and the VM event types) are in [tester/docs/testItemDefinitions.json](tester/docs/testItemDefinitions.json).

### 5. Start the pipeline

```bash
curl -X POST http://localhost:8080/gobbler/pipeline/start
```

### 6. Send test data with the runner

The `tester/runner` tool generates random items and streams them to Gobbler. It registers definitions, starts the pipeline, sends batches on a ticker, and stops the pipeline cleanly on exit.

```powershell
# Run from the repository root
go run ./tester/runner -url http://localhost:8080 -types alpha,beta,gamma
go run ./tester/runner -url http://localhost:8080 -types alpha,beta,gamma -batch 20 -interval 500ms
go run ./tester/runner -url http://localhost:8080 -types alpha,beta,gamma -total 1000
```

CLI flags:

| Flag | Default | Description |
|---|---|---|
| `-url` | `http://localhost:8080` | Base URL of the Gobbler server |
| `-types` | `alpha,beta,gamma` | Comma-separated item type names to generate |
| `-batch` | `10` | Items per POST request |
| `-interval` | `1s` | Pause between requests |
| `-total` | `0` | Stop after N items sent; 0 = unlimited |
| `-seed` | `0` | RNG seed; 0 = time-based |

The runner requires the pipeline to be already **configured but not yet running** when it starts — it calls `pipeline/start` itself and `pipeline/stop` on exit.

> For details on the generator design and supported item types see [tester/docs/generator-design.md](tester/docs/generator-design.md).

### 7. Verify blob connectivity (optional)

Before running in blob mode, you can validate Azure Storage credentials with the standalone diagnostic tool:

```powershell
# Populate tester/secrets.json with {"accountName":"...","accountKey":"..."} first
go run ./tester/blobtest
```

Each access step is printed individually so failures are easy to pinpoint.

