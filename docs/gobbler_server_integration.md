# Creating Gobbler Server and Gobbler Management Portal

## Top-level objectives

The top-level objective is to create a portal which a user/tenant can use to:
- deploy
- configure
- manage, and
- monitor
a set of Gobbler Servers

## What is Gobbler Server

Gobbler Server is the next/extended version of `gobbler` that integrates `gobbler-query` into `gobbler`,
so that the data collected by an instance of Gobbler can be queried via its REST endpoint (which does not preclude querying that data via the gobbler-query CLI with a catalog file).

## Monitoring user's gobbler servers

The broader objective is to allow an owner to run and monitor **multiple independent Gobbler
instances** — each owning its own ingestion pipeline, storage, and query endpoint — while
centralising their operational telemetry in a single **logging Gobbler** server. Query
integration is a prerequisite for that: once `GET /gobbler/query` exists on every instance,
the owner can interrogate any instance's data directly and can query the logging server to
correlate telemetry across all instances.

---

## gobbler and gobbler-query integration

The `gobbler-query` library (`github.com/kozwoj/gobbler-query`) executes GQL queries against
CSV files written by gobbler. Its entry point is:

```go
api.Execute(q string, cat catalog.Catalog, batchSize int) (*api.Result, error)
```

`catalog.Catalog` is a `map[string]*catalog.TableEntry` — a mapping from item type name to
where its data lives (directory path for file mode, Azure container for blob mode). In the CLI
(`gq`) the operator manages this mapping manually in a `catalog.json` file.

In the integrated case, gobbler already knows this mapping from its own runtime state, so no
separate catalog file is needed.

### 1. Source of truth for the query catalog: filesystem discovery

The query handler builds the `catalog.Catalog` at query time by **scanning `OutputDir`** for
type data directories, not from `s.definitions`.

```
OutputDir/
  alpha-folder/           ← StorageBucket = "alpha-folder"
    alpha.json            ← TypeName = "alpha" (read from JSON "name" field)
    2026-05-01_...csv
  beta/
    beta.json
    ...
```

For each subdirectory of `OutputDir`, the handler looks for a `{name}.json` file (written by
`FileWriter` at pipeline start). Each one it finds becomes a `catalog.TableEntry`.

**Why not use `s.definitions`?** Definitions can be removed while historical CSV data remains.
A type removed from the active definition list is still fully queryable — its CSV files and
`{typeName}.json` are untouched on disk.

**Why not an in-memory query catalog?** It would be lost on server restart, requiring extra
bookkeeping that the filesystem already provides for free.

### 2. Precondition for querying

The only server precondition for `GET /gobbler/query` is that the pipeline is **configured**
(`s.config != nil`), meaning `OutputDir` (file mode) or blob credentials (blob mode) are known.

The pipeline does **not** need to be running. Historical data is queryable even after
`pipeline/stop`.

Ingestion has its own unchanged preconditions: configured + definitions registered + running.

### 3. Restart recovery

Gobbler is stateless on restart — all in-memory state (`s.config`, `s.definitions`, `s.types`)
is lost. The existing recovery pattern is to re-run a setup script.

| Goal after restart | Required calls |
|---|---|
| Resume ingestion | `pipeline/configure` → `definition/add` (all types) → `pipeline/start` |
| Resume querying only | `pipeline/configure` only |

When `pipeline/start` runs and `NewFileWriter` is called for a type that already has a
storage directory:
- `os.MkdirAll` is a no-op (directory exists) ✅
- `os.WriteFile` silently overwrites `{typeName}.json` with the same content ✅
- Existing CSV files are untouched ✅

So re-running the setup script after a restart is safe and idempotent, provided the item
definitions are unchanged.

### 4. Schema change risk (known limitation)

If a definition's schema changes between runs (column added, renamed, or type changed),
`NewFileWriter` silently overwrites `{typeName}.json` with the new schema. The query engine
will then apply the new schema to old CSV files that have a different column layout, producing
wrong results or a parse error.

**Gobbler has no schema evolution guard.** This is a known limitation. Mitigation: treat a
schema change as a new type (use a new type name). Do not reuse a type name with a different
column layout.

## Multi-instance monitoring: instance identity in telemetry

An owner may run several "db" Gobbler instances (each with its own storage) and route all of
their operational telemetry to a single logging Gobbler server via gobbler-client. To make that
telemetry queryable per source, every telemetry item must carry the identity of the instance
that produced it.

**`InstanceName` in `pipeline/configure`**: a new `instanceName` string field is added to
`Config` and the configure request body. It is operator-supplied and should be unique across
all instances sharing a logging server (e.g. `"db-prod-1"`, `"db-staging"`).

- If `instanceName` is omitted, the server falls back to `os.Hostname()`, so the field is
  always populated.
- When gobbler-client is constructed at `pipeline/start`, `instanceName` is passed to it.
  The client injects it as a fixed `instanceName` field on every telemetry item it emits.
- Every telemetry item type definition registered on the logging server must include an
  `instanceName` string column.
- Queries against the logging server can then filter by instance:
  `trace | where instanceName == "db-prod-1"`

**Stability**: `instanceName` is re-supplied each time `pipeline/configure` is called (i.e.
after a restart). It is not persisted to disk — consistent with Gobbler's stateless-on-restart
design. Changing the name between runs produces a discontinuity in historical telemetry; treat
this the same way as a schema change (avoid it, or accept a clean break).

---

## What needs to be built before designing the portal

### In gobbler instrumentation

- Add `instanceName` to `Config` and the `pipeline/configure` request body; fall back to `os.Hostname()` if omitted
- Modify telemetry item schemas generated by gobbler-client in gobbler to include an `instanceName` string column
- Modify all logging call sites in gobbler instrumentation to pass `instanceName`
- Update gobbler logging server deployment scripts to register the updated telemetry item schemas


### In `gobbler-query`

Add two discovery functions in `query/catalog/catalog.go`:

**`DiscoverFileCatalog(outputDir string) (Catalog, error)`**:
- Walk subdirectories of `outputDir`
- In each subdir, look for a `*.json` file (there should be exactly one per type)
- Parse the `"name"` field; build `TableEntry{TypeName, StorageBucket=subdir, Mode=file, OutputDir}`
- Return the assembled `Catalog`

**`DiscoverBlobCatalog(accountName, accountKey string) (Catalog, error)`**:
- List all containers using `azblob.Client.NewListContainersPager`
- For each container, list blobs to find the `*.json` schema blob
- Download it, read the `"name"` field for the type name
- Build `TableEntry{TypeName, StorageBucket=containerName, Mode=blob, AccountName, AccountKey}`
- Return the assembled `Catalog`

### In `gobbler`

Add `github.com/kozwoj/gobbler-query` to `go.mod` (local `replace` directive during dev).

Add `server/query_routes.go`:
- `GET /gobbler/query?q=<gql>` handler
- Calls `catalog.DiscoverFileCatalog(s.config.OutputDir)`
- Passes the catalog to `api.Execute(q, cat, 0)`
- Serializes `api.Result` as a JSON array of row objects (`[{"col": val, ...}, ...]`)
- Error mapping: parse/validation errors → 400, execution errors → 500, not configured → 409

Register the route in `server/http.go`.

---

## Blob mode

`gobbler-query` already supports blob queries via `BlobTableReader`, which downloads
`{typeName}.json` directly from the Azure container as part of its own initialisation — no
separate `LoadSchema` call is needed. Blob query execution is already fully implemented.

The only additional piece needed for blob integration is catalog discovery:
`catalog.DiscoverBlobCatalog(accountName, accountKey string) (Catalog, error)` must:
1. List all containers in the storage account using `azblob.Client.NewListContainersPager`
2. For each container, list blobs to find the `*.json` schema blob
3. Download it, read the `"name"` field to get the type name
4. Build `TableEntry{TypeName, StorageBucket=containerName, Mode=blob, AccountName, AccountKey}`

Blob mode is **not deferred** — it ships with the initial integration.

---

## Open questions

1. **Result format**: JSON array of row objects is user-friendly but loses null distinction
   (null vs zero-value). Should the response include an explicit null indicator? Options:
   - Use JSON `null` for null cells (standard, unambiguous) ← preferred
   - Separate `nulls` matrix alongside `rows` (matches `api.Result` internal format)

2. **Query timeout**: long-running queries block the HTTP request. Should there be a server-side
   timeout (e.g., via `context.WithTimeout`)? If yes, what default?

## Portal design

After the above has been implemented and tested, the next phase will cover:
- mapping portal functionality to REST interfaces
- designing basic UI — left-hand side menu + menu-selection-specific right-hand frames
