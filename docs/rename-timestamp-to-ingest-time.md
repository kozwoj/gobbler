# Discussion Note: Rename `timestamp` Column to `ingest_time`

**Date:** 2026-07-02  
**Status:** Proposed — pending name and sequence agreement  
**Affects:** gobbler · gobbler-query · gobbler-client

---

## Problem

Gobbler prepends a system-managed datetime column to every stored CSV record and registers it as the first column in every `{typeName}.json` schema file. This column has historically been named `timestamp`.

Two issues with that name:

1. **Collision with eventing systems.** Most eventing and streaming platforms (EventHub, Kafka, Azure Monitor, etc.) already define a field called `timestamp` as an event-level metadata property. When Gobbler-produced CSVs are ingested downstream, the name collides.
2. **Semantic inaccuracy.** The value is not a generic timestamp — it is specifically the *ingest time*: the moment Gobbler validated and converted the item to CSV. Calling it `timestamp` obscures that meaning.

## Proposed new name

**`ingest_time`**

Rationale for the specific form:
- `ingest-time` — hyphen is invalid in most DB column name contexts (Kusto, DuckDB, SQL).
- `ingesttime` — valid but harder to read.
- `ingest_time` — underscore separator; consistent with standard DB column naming conventions; immediately descriptive.

---

## Impact by repository

### gobbler (origin of the change)

**Functional — must change:**

| File | Change |
|------|--------|
| `items/errors.go` | Error message string: `"\"timestamp\" is a reserved name"` → `"\"ingest_time\""` |
| `items/definition.go` | Two `if name == "timestamp"` guards (type-name check and column-name check) → `"ingest_time"` |
| `items/definition.go` | `storedColumn{Name: "timestamp", Type: "datetime"}` → `"ingest_time"` — **this is the value written into all `{typeName}.json` files and prepended to every CSV record** |

**Test assertions — must match new column name:**

| File | Change |
|------|--------|
| `items/definition_test.go` | Reserved-name rejection test inputs; column-name assertion (`first.Name != "timestamp"`); column struct `{Name: "timestamp", ...}` |
| `writers/writers_logging_test.go` | Two column struct assertions `{Name: "timestamp", Type: "datetime"}` |

**Documentation:**

| File | Change |
|------|--------|
| `docs/REST-commands.md` | Two explicit mentions of `"timestamp"` as the reserved name |
| `README.md` | Three occurrences: prose description of the column, reserved-name note, and the example `{typeName}.json` schema block |

**No change needed:** internal Go variable and parameter names (`timestamp string` in `ConvertItem`, `timestamp :=` in ingest handler, `const fixedTimestamp` in tests). These are purely local identifiers and do not affect stored data.

---

### gobbler-query

gobbler-query reads `{typeName}.json` schema files produced by gobbler and parses the CSV column at position 0 as the ingest-time column. The column name `"timestamp"` appears throughout — in static test fixtures, in the test data generator, and across many test assertions.

**Functional — must change (affect files read from disk at runtime):**

| File | Change |
|------|--------|
| `testdata/requests/requests.json` | `"name": "timestamp"` → `"ingest_time"` |
| `testdata/users/users.json` | same |
| `cmd/testgen/main.go` | Two schema struct literals `{Name: "timestamp", Type: "datetime"}` — the generator that produces test data files |

**Test assertions — must match new column name:**

| File | Approximate volume | Notes |
|------|-------------------|-------|
| `query/logical/infer_test.go` | ~27 occurrences | Column name in every test schema (`mkCol("timestamp", ...)`), sort/project/filter expressions (`FieldRef{Name: "timestamp"}`), join schemas, and string assertions. Heaviest single file. |
| `query/source/schema_test.go` | 3 spots | Column struct literal and two `!= "timestamp"` string assertions |
| `query/source/file_reader_test.go` | 2 spots | Column struct literal and one assertion string |
| `query/source/builders_test.go` | 1 spot | Column struct literal |
| `api/execute_test.go` | 1 spot | Inline JSON fixture `{"name": "timestamp", "type": "datetime"}` |

**Documentation:**

| File | Occurrences | Notes |
|------|-------------|-------|
| `cmd/testgen/testgen.md` | 9 | Documents `timestamp` as column 0 throughout |
| `docs/execution-pipeline.md` | 5 | 1 prose mention + 4 in example query strings (`project timestamp, region`) |
| `README.md` | 1 | Prose line explicitly naming the column |

**No change needed:** `query/source/pruning.go`, `pruning_test.go`, `blob_reader.go`, `file_reader.go` — all occurrences of "timestamp" in these files refer to the *filename* datetime prefix (`YYYY-MM-DD_HH-MM-SS.mmm_<typeName>.csv`), not to the stored column name.

---

### gobbler-client

**No changes required.**

gobbler-client is a sending library. It constructs and transmits item payloads to a Gobbler server; it has no knowledge of what column Gobbler prepends to stored CSVs. The single occurrence of "timestamp" in its README (`"timestamped CSV items"`) describes file output format, not the column name.

---

## Summary

| Repository | Source/test files | Doc files | Notes |
|------------|:-----------------:|:---------:|-------|
| gobbler | 4 | 2 | Origin; cleanest change |
| gobbler-query | 8 | 3 | `infer_test.go` is densest (27 hits) |
| gobbler-client | 0 | 0 | No changes |

**Total:** 12 source/test files, 5 documentation files across two repositories.

## Backward compatibility

Any `{typeName}.json` schema files and CSV files produced by a gobbler instance *before* this rename will retain `timestamp` as the first column header in the schema. After the rename, new schema files will use `ingest_time`. gobbler-query would need to read both old and new data directories — if backward compatibility with existing stored data is required, a compatibility shim (accepting either name as column 0) should be considered before committing the change.

## Proposed implementation sequence

All tests should pass after each step.

1. **gobbler** — `items/errors.go`: update error message string
2. **gobbler** — `items/definition.go`: update two guards and the `storedColumn` name
3. **gobbler** — `items/definition_test.go`, `writers/writers_logging_test.go`: update assertions → run `go test ./...` in gobbler
4. **gobbler** — `docs/REST-commands.md`, `README.md`: update documentation
5. **gobbler-query** — `testdata/requests/requests.json`, `testdata/users/users.json`: update static fixtures
6. **gobbler-query** — `cmd/testgen/main.go`: update generator schemas; regenerate test data if needed
7. **gobbler-query** — all five test files with column-name assertions → run `go test ./...` in gobbler-query
8. **gobbler-query** — `cmd/testgen/testgen.md`, `docs/execution-pipeline.md`, `README.md`: update documentation
