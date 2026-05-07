# Test Item Generator — Design Note

## Item types to support

Two sources of definitions:

| Source | Types |
|---|---|
| `testDefinitions.json` | `alpha` (string, int, datetime), `beta` (string, bool, real), `gamma` (int, string, dynamic) |
| `itemDefinitionExamples.json` | `allscalars`, `somescalars`, `vm-shutdown`, `vm-start`, `vm-reboot` |

Generators for `alpha`, `beta`, `gamma` already exist in `tester/`.  
Generators for the `itemDefinitionExamples.json` types need to be written.

## Item generation strategy

Each type has a dedicated generator (`*Generator` struct) with a `GenerateItem()` method.  
Random values per column type:

| Gobbler type | Generation rule |
|---|---|
| `string` | random alphanumeric, configurable length range |
| `int` | random in configurable range (default 0–10 000) |
| `real` | random float in configurable range, 2 decimal places |
| `bool` | 50/50 |
| `datetime` | random timestamp within last N days (default 30) |
| `timespan` | random from a fixed set: `"1s"`, `"30s"`, `"5m"`, `"1h"`, `"1d"` |
| `dynamic` | small JSON object with a fixed schema per type |

Optional fields: omitted ~30% of the time (or configurable omit probability).

## Coordinator

A `Runner` struct selects types and dispatches to generators:

```
Runner
  generators  map[string]ItemGenerator   // keyed by type name
  weights     map[string]int             // relative frequency per type
```

`ItemGenerator` interface:
```go
type ItemGenerator interface {
    GenerateWrapped() map[string]any   // returns {"typename": {fields}}
}
```

note: we should be able to customize each item generator to make the items more realistic. for example some of the string values can be taken for a string array, like rebootReason or shutdownReason in vm vm-shutdown and vm-reboot. 

Selection: weighted random — each generation cycle picks a type proportionally to its weight (default weight 1 = equal distribution).

Batch assembly: the Runner fills a `[]map[string]any` up to `batchSize` items, potentially mixing types in a single batch (valid per Gobbler's ingest contract).

## Throttling

Two knobs:
- **`batchSize`** — number of items per POST request (default 10)
- **`interval`** — pause between batches (default 1 s)

Effective throughput: `batchSize / interval` items/second.  
A `time.Ticker` drives the loop; one batch is sent per tick.

Optional **`totalItems`** limit (0 = run until stopped via Ctrl-C).

## Configuration (CLI flags)

```
-endpoint   string   Gobbler ingest URL (default "http://localhost:8080/gobbler/ingest")
-types      string   Comma-separated type names to generate (default "alpha,beta,gamma")
-batch      int      Items per request (default 10)
-interval   duration Pause between requests (default 1s)
-total      int      Stop after N items sent; 0 = unlimited (default 0)
-seed       int      RNG seed; 0 = time-based (default 0)
```

## Open questions

1. Should the generator also call `definition/add` to register types before ingesting, or assume they are pre-registered?
- answer: the pipeline *configuration* (mode, outputDir, accountName, etc.) is done externally so it can vary per experiment.
  The runner is responsible for:
  - verifying the server is reachable and configured (`GET /gobbler/pipeline/status` → abort if `configured` is not `true` or pipeline is already running)
  - registering item definitions (`POST /gobbler/definition/add`) for each requested type
  - starting the pipeline (`POST /gobbler/pipeline/start`)
  - stopping the pipeline (`POST /gobbler/pipeline/stop`) when done (or on interrupt)
  
2. Do we want per-type weight configuration on the CLI, or is equal distribution enough for now?
- answer: for now we can have equal distribution

3. Should the runner live in `tester/` (library) + `tester/cmd/` (main), or directly as a standalone `cmd/tester/` at the repo root?
- answer: the runner should live in `tester/` directory in `tester/runner`. it should be similar to how blobtest is placed. 
