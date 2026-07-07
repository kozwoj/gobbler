# Creating Gobbler Server and Gobbler Management Portal

## Top-level objective

The top-level objective is to create a portal which a user/tenant can use to:
- deploy
- configure
- manage, and
- monitor
a set of Gobbler Servers

An intermediate objective is to create Gobbler Server, which is an integration of Gobbler ingestion server and Gobbler query engine. 

## What is Gobbler Server

Gobbler Server is the next/extended version of `gobbler` that integrates `gobbler-query` into `gobbler`, so that the data collected by an instance of Gobbler can be queried via its REST endpoint (which does not preclude querying that data via the gobbler-query CLI with a catalog file).

## Monitoring user's gobbler servers

The broader objective is to allow an owner/user to run and monitor **multiple independent Gobbler instances** — each owning its own ingestion pipeline, storage, and query endpoint — while centralising their operational telemetry in a single **logging Gobbler** server. Query integration is a prerequisite for that: once `POST /gobbler/query` exists on every instance, the owner can interrogate any instance's data directly and can query the logging server to correlate telemetry across all instances.

---

## gobbler and gobbler-query integration

The `gobbler-query` library (`github.com/kozwoj/gobbler-query`) executes GQL queries against CSV files written by gobbler. Its entry point is:

```go
api.Execute(q string, cat catalog.Catalog, batchSize int) (*api.Result, error)
```

`catalog.Catalog` is a `map[string]*catalog.TableEntry` — a mapping from item type name to where its data lives (directory path for file mode, Azure container for blob mode). In the CLI (`gq`) the operator manages this mapping manually in a `catalog.json` file stored in `<home>/.gobbler/` directory.

In the integrated case gobbler can build this mapping from its runtime state and/or storage, so no separate catalog file is needed.

### 1. Source of truth for the query catalog: filesystem discovery

The initial version of `catalog.Catalog` should be built by **scanning `OutputDir`** for type data directories and files created by gobbler. This can be done at two points:
1. when gobbler is configured and is given the `mode` and `OutputDir` to use, or
2. the query handler builds it during the first query.  

```
OutputDir/
  alpha-folder/           ← StorageBucket = "alpha-folder"
    alpha.json            ← TypeName = "alpha" (read from JSON "name" field)
    2026-05-01_...csv
  beta/
    beta.json
    ...
```

For each subdirectory of `OutputDir` we look for a `{name}.json` file (written by `FileWriter` at pipeline start). Each such file becomes a `catalog.TableEntry`.

Once item definitions are added to gobbler the `catalog.Catalog` should be updated if there is no storage for it yet.

**Why not use `s.definitions`?** Definitions should not be used for two reason: 
1. at run time and after the pipeline has been started a definition can be removed while historical CSV data remains. A type removed from the active definition list is still fully queryable — its CSV files and `{typeName}.json` are untouched on disk.
2. after restart and configuration but before the pipeline was started a query can still be issued against the data collected prior to restart. 

### 2. Precondition for querying

The only server precondition for `POST /gobbler/query` is that the pipeline is **configured** (`s.config != nil`), meaning `OutputDir` (file `mode`) or blob credentials (blob `mode`) are known.

The pipeline does **not** need to be running. Historical data is queryable even after `pipeline/stop`.

Ingestion has its own unchanged preconditions: configured + definitions registered + running.

### 3. Restart recovery

Gobbler is stateless on restart — all in-memory state (`s.config`, `s.definitions`, `s.types`) is lost. The existing recovery pattern is to re-run a setup script.

| Goal after restart | Required calls |
|---|---|
| Resume ingestion | `pipeline/configure` → `definition/add` (all types) → `pipeline/start` |
| Resume querying only | `pipeline/configure` only |

When `pipeline/start` runs and `NewFileWriter` is called for a type that already has a storage directory:
- `os.MkdirAll` is a no-op (directory exists) ✅
- `os.WriteFile` silently overwrites `{typeName}.json` with the same content ✅
- Existing CSV files are untouched ✅

So re-running the setup script after a restart is safe and idempotent, provided the item definitions are unchanged.

### 4. Schema change risk (known limitation)

If a definition's schema changes between runs (column added, renamed, or type changed), `NewFileWriter` silently overwrites `{typeName}.json` with the new schema. The query engine will then apply the new schema to old CSV files that have a different column layout, producing wrong results or a parse error.

**Gobbler has no schema evolution guard.** This is a known limitation. Mitigation: treat a schema change as a new type (use a new type name). Do not reuse a type name with a different column layout.

## Multi-instance monitoring: instance identity in telemetry

An owner may run several "db" Gobbler instances (each with its own storage) and route all of their operational telemetry to a single logging Gobbler server via gobbler-client instrumentation. To make that telemetry queryable per source, every telemetry item must carry the identity of the "db" Gobbler instance that produced it.

**`InstanceName` in `pipeline/configure`**: a new `instanceName` string field is added to `Config` and the configure request body. It is operator-supplied and should be unique across all instances sharing a logging server (e.g. "db-prod-1"`, `"db-staging"`).

- `instanceName` is a required filed in the configuration.
- When gobbler-client is constructed at `pipeline/start`, `instanceName` is passed to it.
- Every gobbler telemetry item type definition includes `instanceName` string column. The client fills that column with the value from pipeline configuration.
- Queries against the logging server can then filter by instance:   `trace(*) | where instanceName == "db-prod-1"`

**Stability**: `instanceName` is re-supplied each time `pipeline/configure` is called (i.e. after a restart). It is not persisted to disk — consistent with Gobbler's stateless-on-restart design. Changing the name between runs produces a discontinuity in historical telemetry; treat this the same way as a schema change (avoid it, or accept a clean break).

---

## What needs to be built before designing the portal

### In gobbler instrumentation (**done**)

- Add `instanceName` to `Config` and the `pipeline/configure` request body; fall back to `os.Hostname()` if omitted
- Modify telemetry item schemas generated by gobbler-client in gobbler to include an `instanceName` string column
- Modify all logging call sites in gobbler instrumentation to pass `instanceName`
- Update gobbler logging server deployment scripts to register the updated telemetry item schemas.
- Run logging tests. 


### In `gobbler-query`

No changes required. `gobbler-query` already exposes everything needed:
- `catalog.Catalog` (`map[string]*catalog.TableEntry`) and `catalog.TableEntry` — importable types used to describe the query catalog
- `api.Execute(q string, cat catalog.Catalog, batchSize int) (*api.Result, error)` — the query entry point

Catalog discovery (walking `OutputDir` or listing Azure containers) will be implemented inside gobbler, not gobbler-query. This keeps gobbler-query unchanged and usable as a standalone query engine.

### In `gobbler`

Add `github.com/kozwoj/gobbler-query` to `go.mod` with a local `replace` directive during development (`replace github.com/kozwoj/gobbler-query => ../gobbler-query`). Remove the directive and pin a tagged version before the first production release.

#### New package: `gobbler/query/`

All query logic that is not HTTP-specific lives here, keeping it independently testable.

**`query/catalog.go`** — catalog discovery:

```go
type schemaFile struct {
	Name           string `json:"name"`
	OrderedColumns []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"orderedColumns"`
}
```

`BuildFileCatalog(outputDir string) (catalog.Catalog, error)`:
- Walk subdirectories of `outputDir`
- In each subdir find the `*.json` schema file (exactly one per type, written by `FileWriter`)
- Parse it into `schemaFile`; read `Name` as the type name and the subdir name as `StorageBucket`
- Build `catalog.TableEntry{TypeName, StorageBucket=subdir, Mode=StorageModeFile, OutputDir}`
- Return the assembled `catalog.Catalog`

`BuildBlobCatalog(accountName, accountKey string) (catalog.Catalog, error)`:
- List all containers using `azblob.Client.NewListContainersPager`
- For each container, list blobs to find the `*.json` schema blob
- Download and parse it into `schemaFile`; use container name as `StorageBucket`
- Build `catalog.TableEntry{TypeName, StorageBucket=containerName, Mode=StorageModeBlob, AccountName, AccountKey}`
- Return the assembled `catalog.Catalog`

**`query/result.go`** — result serialisation:

`SerializeResult(r *api.Result) ([]byte, error)`:
- Converts `api.Result` to a JSON array of row objects: `[{"col": val, ...}, ...]`
- Uses JSON `null` for null cells (resolved open question — see below)

#### HTTP layer: `server/query_routes.go`

Thin handler, delegates all logic to `gobbler/query`:
- `POST /gobbler/query` handler
- Reads `query` from the JSON request body; returns 400 if missing or empty
- Returns 409 if `s.config == nil` (pipeline not yet configured)
- Calls `query.BuildFileCatalog(s.config.OutputDir)` or `query.BuildBlobCatalog(...)` based on `s.config.Mode`
- Calls `api.Execute(q, cat, 0)`
- On parse/validation error → 400; on execution error → 500
- On success: writes `application/json` body via `query.SerializeResult`

Register the route in `server/http.go`.

---

## Blob mode

`gobbler-query` already supports blob queries via `BlobTableReader`, which downloads
`{typeName}.json` directly from the Azure container as part of its own initialisation — no
separate `LoadSchema` call is needed. Blob query execution is already fully implemented.

The only additional piece needed for blob integration is catalog discovery:
`BuildBlobCatalog(accountName, accountKey string) (catalog.Catalog, error)` (in `gobbler/query/catalog.go`) must:
1. List all containers in the storage account using `azblob.Client.NewListContainersPager`
2. For each container, list blobs to find the `*.json` schema blob
3. Download it, read the `"name"` field to get the type name
4. Build `TableEntry{TypeName, StorageBucket=containerName, Mode=blob, AccountName, AccountKey}`

Blob mode is **not deferred** — it ships with the initial integration.

---

## Catalog lifecycle

The query catalog is stored as `s.catalog catalog.Catalog` on the `Server` struct, protected by `s.catalogMu sync.RWMutex`. It is never rebuilt per query.

| Event | Catalog action |
|---|---|
| `pipeline/configure` | Full build from disk (file mode) or blob scan (blob mode) — captures all historical data before the pipeline is running |
| `pipeline/start` | Full rebuild — FileWriters/BlobWriters have just created new type dirs/containers |
| Hot-add (`pipeline/writers/add`) | Add single entry for the new type — no full rescan |
| `pipeline/stop` | No change — data remains on disk/blob, still fully queryable |
| Query handler | Read under `RLock` |

`pipeline/configure` and `pipeline/start` happen at most once per setup script run, so a full rebuild there is acceptable. Hot-add is the common runtime event; a targeted single-entry update keeps it cheap. The server has all required information (TypeName, StorageBucket, Mode, credentials) to construct the entry directly after a writer is successfully created.

### Blob catalog validation

When `BuildBlobCatalog` scans a container and finds a `*.json` blob, it validates:
1. The blob downloads successfully
2. The content is valid JSON
3. The JSON has a non-empty `name` field
4. `orderedColumns` is non-empty
5. Every column `type` is a recognised gobbler type (`bool`, `datetime`, `dynamic`, `int`, `real`, `string`, `timespan`)

**If all `*.json` blobs in a container fail validation, the container is silently skipped** — it is not a gobbler container. Only containers where at least one blob passes all five checks contribute an entry to the catalog. A container where the blob is present but its JSON is structurally malformed (fails checks 1–2) is skipped with a warning log rather than returning an error, to avoid one corrupted container blocking access to all others.

---

## Open questions

1. **Result format**: resolved — use JSON `null` for null cells (standard, unambiguous).

2. **Query timeout**: deferred to a later iteration. No server-side timeout in the initial implementation.

---

## Test data strategy

Server integration tests generate all data live during the test: configure gobbler with a temp `OutputDir`, add a simple definition, start the pipeline, ingest a handful of items via `POST /ingest`, then query via `POST /gobbler/query`. The temp dir is cleaned up after each test.

Rationale: gobbler's query tests validate the **integration** of gobbler-query into gobbler (HTTP layer, catalog discovery, error mapping, stop-then-query). They do not re-test query engine correctness — that is gobbler-query's responsibility and is already covered there. Live-ingested data is sufficient and keeps gobbler fully self-contained with no dependency on gobbler-query's testdata.

## Implementation steps

All tests should pass after each step.

1. **gobbler** — `go.mod`: add `gobbler-query` dependency with local `replace` directive
2. **gobbler** — `query/catalog.go`: implement `BuildFileCatalog`; add unit tests
3. **gobbler** — `query/catalog.go`: implement `BuildBlobCatalog`; add integration test (gated on secrets)
4. **gobbler** — `query/result.go`: implement `SerializeResult`; add unit tests; run `go test ./query/...`
5. **gobbler** — `server/query_routes.go`: implement `GET /gobbler/query/tables` handler; register in `server/http.go`
6. **gobbler** — `server/query_routes.go`: implement `POST /gobbler/query` handler; register in `server/http.go`
7. **gobbler** — add `catalog` field to `Server` struct; wire catalog build into configure, start, and hot-add paths
8. **gobbler** — add server integration tests for both query endpoints; run `go test ./...`

**note**: tag gobbler-query with version v0.0.1 before removing `replace` directive from go.mod.

## Portal design

After the above has been implemented and tested, the next phase will cover:
- mapping portal functionality to REST interfaces
- designing basic UI — left-hand side menu + menu-selection-specific right-hand frames
