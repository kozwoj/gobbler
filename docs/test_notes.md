# Test Strategy Notes — REST Route Handlers

This note records the agreed test scenarios and approach for validating the route handler implementations described in `docs/implementation_notes.md`.

**Status: proposed — not yet implemented.**

---

## Approach options (not yet decided)

**Option 1 — `.http` file (VS Code REST Client)**
Update/replace `tester/docs/REST_boudaryServicet.http` with requests matching the current routes and the scenarios below. Manual execution; results are eyeballed.

**Option 2 — Go `httptest` unit tests in `server/`**
Table-driven tests using `net/http/httptest` and `httptest.NewRecorder`. No live server needed for most scenarios; file-mode tests write to a temp dir. Assertions are in code.

**Option 3 — Both**
`.http` for exploratory/smoke testing; `httptest` tests for the repeatable regression suite.

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
| C6 | `GET /gobbler/pipeline/status` | `running: true`, `activeTypes: [alpha, beta]` |
| C7 | Ingest a batch of valid `alpha` and `beta` items | `{"ingested": N, "rejected": []}` |
| C8 | Wait, then check output dir | CSV file(s) present under `outputDir/alphaFolder` and `outputDir/bettaFolder` |
| C9 | `POST /gobbler/pipeline/stop` | 200; files flushed and closed |

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
| E1 | Start pipeline with only `alpha` registered | Running with 1 active type |
| E2 | `POST /gobbler/definition/add` with `gamma` | 200; `gamma` appears in `status.activeTypes` |
| E3 | Ingest `gamma` items | Written to disk |
| E4 | `POST /gobbler/definition/remove` `{"typeName": "gamma"}` | 200; `gamma` gone from `activeTypes`; file flushed |
| E5 | Ingest `gamma` items again | All appear in `rejected` (unknown type) |

### Category F — Rotate

| # | Call | Expected |
|---|---|---|
| F1 | Ingest some `alpha` items (below batch threshold) | Buffer partially filled |
| F2 | `POST /gobbler/pipeline/rotate` `{"typeName": "alpha"}` | 200; current file closed |
| F3 | Ingest more `alpha` items | Written to a new timestamped file |

### Category G — Lifecycle edge cases

| # | Call | Expected |
|---|---|---|
| G1 | `POST /gobbler/pipeline/start` when already running | 409 |
| G2 | `POST /gobbler/pipeline/configure` when running | 409 |
| G3 | `POST /gobbler/pipeline/stop` when not running | 409 |
| G4 | `POST /gobbler/definition/add` with duplicate name | 409 |
| G5 | `POST /gobbler/definition/remove` with non-existent name | 404 |
| G6 | Stop → reconfigure with different `outputDir` → re-add definitions → start | 200 at each step; writes go to new dir |

---

## Test data

Existing files in `tester/docs/` that can be reused:

- `testDefinitions.json` — `alpha`, `beta`, `gamma` item type definitions
- `inputExamples.json` — sample ingest payloads covering `allscalars`, `vmShutdown`, `vmReboot`, `somescalars`

The generators in `tester/` (`alpha_generator.go`, `beta_generator.go`, `gamma_generator.go`) produce random valid items and can be used to drive load tests.
