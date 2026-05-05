Read [](file:///c%3A/Users/kozac/Source/go/gobbler/docs/gobbler-logging-client.md#1-1), lines 1 to 180

Here is a proposed step-by-step implementation plan, ordered so each step is independently testable before moving to the next:

---

## Gobbler Client SDK — Implementation Steps

### ~~Step 1 — Module scaffold~~ ✓ DONE
- Create `client/go.mod` (`module github.com/kozwoj/gobbler-client`, `go 1.24.1`)
- Create `client/client.go` with the `Client` interface and stub implementations
- Create `client/options.go` with option types (no logic yet)
- Add `replace` directive to `gobbler/go.mod` pointing at `./client`

**Test**: `go build ./client/` passes; nothing else yet.

---

### ~~Step 2 — `nopClient` and `Nop()`~~ ✓ DONE
- Implement `nopClient` struct satisfying `Client` — all methods return nil
- Implement `Nop() Client`
- Write unit tests: `Log`, `Flush`, `Close`, `SwapServer` on a nop client all return nil, no panic

**Test**: pure unit tests, no network.

---

### ~~Step 3 — Options and defaults~~ ✓ DONE
 - Implement `WithTypes(names ...string)`, `WithWriterBatchSize(n int)`, `WithFlushInterval(d time.Duration)`
 - Implement an internal `config` struct populated by options, with defaults (e.g. writerBatchSize=100, flushInterval=10s)
- Write unit tests: verify config is applied and defaults kick in when options are omitted

**Test**: pure unit tests, no network.

---

### ~~Step 4 — Buffer and `Log`~~ ✓ DONE
- Implement `realClient` with a `[]bufItem` buffer protected by a mutex
- Implement `Log(typeName string, fields map[string]any) error`
  - Unknown type name → immediate error
  - Append to buffer
  - If `len(buffer) >= writerBatchSize` → call internal `flush()` (implemented as no-op stub for now)
- Write unit tests: unknown type, buffer growth, threshold detection

**Test**: pure unit tests, no network.

---

### ~~Step 5 — Serialisation and HTTP flush~~ ✓ DONE
- Implement internal `flush()`: serialise buffer as `[{"typeName":{fields}}, ...]` JSON, POST to `/gobbler/ingest`
- Implement response parsing: 400 → error (no retry), 200+rejected → error listing rejections, 5xx → hold buffer (return error but do not drain)
- Implement `Flush() error` (public: acquires lock, calls flush, returns error)
- Write unit tests using `httptest.NewServer` to simulate 200, 200+rejected, 400, 500 responses

**Test**: unit tests with a fake HTTP server — no real Gobbler needed.

---

### ~~Step 6 — Background flush goroutine and `Close`~~ ✓ DONE
- Start a ticker goroutine inside `New()` (or a separate `start()` method) that calls flush every `flushInterval`
- Implement `Close() error`: signal goroutine to stop, flush remaining buffer, return any error
- Write unit tests: verify items are flushed after interval elapses; verify buffer is drained on Close; verify Close is idempotent

**Test**: unit tests with `httptest.NewServer` and short flush intervals.

---

### ~~Step 7 — Server validation in `New()`~~ ✓ DONE
- Implement `validateServer(endpoint string, types []string) error`:
  - GET `/gobbler/pipeline/status` → parse `running` bool
  - GET `/gobbler/definition/names` → check all registered types are present
- Call from `New()`: on failure return `Nop()` + error
- Write unit tests using `httptest.NewServer` simulating: not running, missing types, all valid

**Test**: unit tests with a fake HTTP server.

---

### ~~Step 8 — `SwapServer`~~ ✓ DONE
- Implement `SwapServer(newURL string) error`: validate new target, then atomically swap endpoint
- In-progress flush completes against old endpoint; subsequent flushes use new one
- Write unit tests: successful swap routes subsequent flushes to new server; failed validation keeps old server; concurrent flush + swap is race-free (`go test -race`)

**Test**: unit tests with two `httptest.NewServer` instances.

---

### ~~Step 9 — Integration test against a real Gobbler server~~ ✓ DONE
- Added `server/integration_test.go` (`//go:build integration`, package `server`):
  - `TestIntegration_I9_1_New_ValidatesRealServer` — `New()` succeeds against a running server with the type registered
  - `TestIntegration_I9_2_LogFlush_ItemsReachServer` — logs 3 items, flushes, confirms via `pipeline/status` `itemsInBuffer ≥ 3`
  - `TestIntegration_I9_3_SwapServer_RoutesToNewServer` — flushes to srv1, stops it, starts srv2, `SwapServer`, confirms items reach srv2
- Note: test is in `server/` (not `client/`) to avoid circular import (gobbler already imports gobbler-client)

**Run**: `go test -tags integration -run TestIntegration ./server/`

---

### Resolved open questions before starting Step 6
Before implementing `Close` you need a decision on: **does `Close` drain the buffer even if a mid-flush error occurs?** Recommend yes — drain and collect all errors, return the first non-nil. Otherwise items are silently lost on a transient 5xx.