# Test Strategy Notes — REST Route Handlers

This note records the agreed test scenarios and approach for validating the route handler implementations described in `docs/implementation_notes.md`.

**Status: approach decided — implementation not yet started.**

---

## Approach

**Option 2 — Go `httptest` unit tests in `server/`**
Table-driven tests using `net/http/httptest` and `httptest.NewRecorder`. Tests live in `server/server_test.go`. No live server or open ports needed. File-mode tests write to a `t.TempDir()`. Each test that exercises start/stop calls `t.Cleanup(pipeline.Reset)` to prevent package-level state leaking between tests. Runnable with `go test ./server/...` and in CI.

**Option 3 — Live server integration tests** is deferred for later performance and throughput testing.

---

## Test scenarios

### Category A — Pre-condition enforcement (before configure/start)

| # | Call | Expected |
|---|---|---|
| A1 | `GET /gobbler/pipeline/status` | `configured: false`, `running: false` |
| A2 | `POST /gobbler/pipeline/start` | 409 — not configured |
| A3 | `POST /gobbler/definition/add` (valid body) | 200 — definition stored, no pipeline started |
| A4 | `POST /gobbler/pipeline/start` | 409 — not configured (definition exists but no config) |
| A5 | `POST /gobbler/ingest` | 409 — not running |

### Category B — Configure validation

| # | Call | Expected |
|---|---|---|
| B1 | `POST /gobbler/pipeline/configure` — missing `mode` | 400 |
| B2 | `POST /gobbler/pipeline/configure` — `mode: file`, no `outputDir` | 400 |
| B3 | `POST /gobbler/pipeline/configure` — `mode: blob`, missing `accountKey` | 400 |
| B4 | `POST /gobbler/pipeline/configure` — valid file mode | 200, `status: ok` |
| B5 | `GET /gobbler/pipeline/status` after B4 | `configured: true`, `running: false`, correct mode/sizes |

### Category C — Happy path (configure → add → start → ingest → stop)

| # | Call | Expected |
|---|---|---|
| C1 | Configure (file mode, temp `outputDir`) | 200 |
| C2 | Add `alpha` definition (from `tester/docs/testDefinitions.json`) | 200 |
| C3 | Add `beta` definition | 200 |
| C4 | `GET /gobbler/definition/list` | Array containing `alpha` and `beta` |
| C5 | `POST /gobbler/pipeline/start` | 200; output subdirectories created on disk |
| C6 | `GET /gobbler/pipeline/status` immediately after start | `running: true`; `types` map contains entries for `alpha` and `beta`; both show `itemsInBuffer: 0`, `itemsWritten: 0`, `lastFlush` zero, `currentOutput: ""` |
| C7 | Ingest N valid `alpha` items and M valid `beta` items | `{"ingested": N+M, "rejected": []}` |
| C8 | `GET /gobbler/pipeline/status` after short wait (flush tick) | `alpha.itemsWritten == N`, `beta.itemsWritten == M`, `lastFlush` non-zero, `currentOutput` non-empty for both |
| C9 | Check output dir on disk | CSV files present under `outputDir/alphaFolder` and `outputDir/bettaFolder` |
| C10 | `POST /gobbler/pipeline/stop` | 200; files flushed and closed |
| C11 | `GET /gobbler/pipeline/status` after stop | `running: false`, `configured: true`, `types` key absent |

### Category D — Ingest error handling

| # | Call | Expected |
|---|---|---|
| D1 | Ingest item with unknown type name | Entry in `rejected`; `ingested` count unaffected |
| D2 | Ingest item with a missing required field | `rejected` contains field-level error |
| D3 | Ingest item with wrong field type (e.g. string where int expected) | `rejected` contains type error |
| D4 | Ingest mix of valid and invalid items | `ingested` + `rejected` counts equal total submitted |
| D5 | Ingest malformed JSON body | Parse error in `rejected` |

### Category E — Hot-add and hot-remove while running

| # | Call | Expected |
|---|---|---|
| E1 | Start pipeline with only `alpha` registered | Running; `writers` map has only `alpha` entry |
| E2 | `POST /gobbler/definition/add` with `gamma` | 200; `status.types` now contains `gamma` with zeroed stats |
| E3 | Ingest N `gamma` items | `ingested: N`; after flush tick `gamma.itemsWritten == N` in status |
| E4 | `POST /gobbler/definition/remove` `{"typeName": "gamma"}` | 200; `gamma` gone from `status.writers`; its file flushed on disk |
| E5 | Ingest `gamma` items again | All appear in `rejected` (unknown type) |

### Category F — Rotate

| # | Call | Expected |
|---|---|---|
| F1 | Ingest some `alpha` items (below batch threshold) | `status` shows `alpha.itemsInBuffer > 0`, `currentOutput: ""` (no flush yet) |
| F2 | `POST /gobbler/pipeline/rotate` `{"typeName": "alpha"}` | 200; `status` shows `alpha.itemsInBuffer: 0`, `itemsWritten > 0`, `currentOutput: ""` (file closed after rotate) |
| F3 | Ingest more `alpha` items then wait for flush tick | A second timestamped CSV file appears in `outputDir/alphaFolder` |

### Category G — Lifecycle edge cases

| # | Call | Expected |
|---|---|---|
| G1 | `POST /gobbler/pipeline/start` when already running | 409 |
| G2 | `POST /gobbler/pipeline/configure` when running | 409 |
| G3 | `POST /gobbler/pipeline/stop` when not running | 409 |
| G4 | `POST /gobbler/definition/add` with duplicate name | 409 |
| G5 | `POST /gobbler/definition/remove` with non-existent name | 404 |
| G6 | Stop → reconfigure with different `outputDir` → re-add definitions → start | 200 at each step; writes go to new dir |
| G7 | `GET /gobbler/pipeline/status` after stop | `running: false`; `types` key absent from response |

### Category H — Writer stats accuracy

These tests verify that `status.types[T].itemsWritten` matches the count returned by the ingest endpoint.

| # | Steps | Expected |
|---|---|---|
| H1 | Ingest exactly 10 `alpha` items (batch size > 10); wait for flush tick; check status | `alpha.itemsWritten == 10` |
| H2 | Ingest another 15 `alpha` items; wait; check status | `alpha.itemsWritten == 25` (cumulative) |
| H3 | Ingest a mixed batch of 5 valid `alpha` + 3 invalid items; check ingest response and then status | `ingested == 5`, `rejected` len == 3; `alpha.itemsWritten` increased by exactly 5 after flush |
| H4 | Ingest exactly `batchSize` items in one call | Immediate flush triggered (no tick needed); `itemsInBuffer == 0` and `itemsWritten == batchSize` visible in next status call |

---

## Test data

Existing files in `tester/docs/` that can be reused:

- `testDefinitions.json` — `alpha`, `beta`, `gamma` item type definitions
- `inputExamples.json` — sample ingest payloads covering `allscalars`, `vmShutdown`, `vmReboot`, `somescalars`

The generators in `tester/` (`alpha_generator.go`, `beta_generator.go`, `gamma_generator.go`) produce random valid items and can be used to drive load tests.
