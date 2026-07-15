# Integration of gobbler with gobbler-query

Gobbler-query [kozwoj/gobbler-query](https://github.com/kozwoj/gobbler-query) is a standalone query engine and CLI that can used to query collections of CSV files created by gobbler. 

The extended version of `gobbler` integrates `gobbler-query` into `gobbler`, so that the data collected by a configured instance of Gobbler can be queried via a REST endpoint of that instance (which does not preclude querying that data via the gobbler-query CLI with a catalog file).

The entry point of the `gobbler-query` library is:

```go
api.Execute(q string, cat catalog.Catalog, batchSize int) (*api.Result, error)
```

`catalog.Catalog` is a `map[string]*catalog.TableEntry` — a mapping from item type name to where the item's data lives (directory path for file mode, or Azure container for blob mode). In the case of standalone use of `gobbler-query` via the CLI (`gq`) the catalog is created manually and stored in `catalog.json` file in `<home>/.gobbler/` directory.

In the integrated case gobbler can build this mapping from its runtime state and/or storage, so no separate catalog file is needed.

## Initial state of catalog

For the file mode the initial state of `catalog.Catalog` is built by scanning `outputDir` for type-specific directories and files created by gobbler. This is done when gobbler is configured and is given the `mode` and the `outputDir` to use.

```
OutputDir/
  alpha-folder/           ← StorageBucket = "alpha-folder"
    alpha.json            ← TypeName = "alpha" (read from JSON "name" field)
    2026-05-01_...csv
  beta/
    beta.json
    ...
```

For each subdirectory of `outputDir` we look for a `{name}.json` file (written by `FileWriter`). Each such file becomes a `catalog.TableEntry`.

For the blob mode instead of subdirectories of the `outputDir` we look for a `{name}.json` file in every container of the storage account provided in configuration property `accountName`.  

The initial state of the catalog can be used to query historical data even when pipeline has **not**  been started.

## Extending catalog with new types

Once pipeline has been started and new item definitions are added to gobbler the `catalog.Catalog` should be updated, if there is no storage for the item yet.

**Why not use `s.definitions`?** Definitions should not be used for two reason: 
1. at run time and after the pipeline has been started a definition can be removed while historical CSV data remains. A type removed from the active definition list is still fully queryable — its CSV files and `{typeName}.json` are untouched on disk.
2. after restart and configuration but before the pipeline was started, a query can still be issued against the data collected prior to restart. 

## Precondition for querying

The only server precondition for `POST /gobbler/query` is that the pipeline is **configured** (`s.config != nil`), meaning `outputDir` (file `mode`) or blob account and credentials (blob `mode`) are known.

The pipeline does **not** need to be running. Historical data is queryable even after `pipeline/stop`.

## Restart recovery

Gobbler is stateless on restart — all in-memory state (`s.config`, `s.definitions`, `s.types`) is lost. The existing recovery pattern is to re-run a setup script.

| Goal after restart | Required calls |
|---|---|
| Resume ingestion | `pipeline/configure` → `definition/add` (all types) → `pipeline/start` |
| Resume querying only | `pipeline/configure` only |

When `pipeline/start` runs and `NewFileWriter` is called for a type that already has a storage directory:
- `os.MkdirAll` is a no-op (directory exists)
- `NewFileWriter` reads the existing `{typeName}.json` and compares it against the definition being registered. If the stored schema matches the definition exactly (same column names, types, and order), the file is overwritten with the same content and startup proceeds normally
- If the schemas differ, `NewFileWriter` returns an error and `pipeline/start` fails — protecting existing CSV data from being queried with the wrong schema
- Existing CSV files are untouched

So re-running the setup script after a restart is safe and idempotent, provided the item definitions are unchanged.

## Schema change protection

If a definition's schema changes between restarts (column added, renamed, reordered, or type changed), `NewFileWriter` detects the mismatch when comparing the new definition against the existing `{typeName}.json` and returns an error. `pipeline/start` (or hot-add) will fail with a clear message identifying the conflicting column.

The error message identifies the first mismatch found:
- column count difference
- column name mismatch at a given position
- column type mismatch at a given position

## Query execution vs. pipeline execution

Both server's `handleQuery` and `handleIngest` take RLock (shared), so they don't block each other. Only write operations (pipeline/start, pipeline/stop, configure, hot-add) take the exclusive write lock. Therefore slow query (large dataset, complex GQL) uses its own HTTP handler goroutine and its own disk I/O — it has no lock contention with the ingestion pipeline. 