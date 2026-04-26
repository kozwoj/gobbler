# Refactoring: Direct Routing (Remove Central Queue and Dispatcher)

## Date
2026-04-26

## Committed baseline
The current version (before this refactor) is committed to git with the message:
> "committed Gobbler version with central queue and dispatcher enqueuing items with writers. in that version it is hard to handle saturation of either the central queue or any of the writer's queues. also, one writer may block the entire central queue. the next version will have the http handlers enqueue items directly with writers."

---

## Problem with the current architecture

The current pipeline has two stages:

```
ingest handler
    → central queue (chan CSVitem, capacity = centralQueueSize)
        → dispatcher goroutine
            → per-type worker queue (capacity = workerQueueSize)
                → worker goroutine
                    → writer
```

This introduces two silent failure modes:

1. **Central queue full** — `pipeline.Enqueue()` returns `false`. The ingest handler catches this and adds the item to `rejected` with `"pipeline queue full"`. The client is informed, but the message is vague — it does not say which type is the bottleneck.

2. **Per-type worker queue full** — the dispatcher does a non-blocking send into the worker queue and silently drops the item on the `default` branch. The ingest handler has already responded `ingested++` to the client. **The client believes the item was accepted, but it is lost.**

Additionally, a single slow writer can saturate its per-type queue, which blocks the dispatcher goroutine, which backs up the central queue, which rejects items of *all* types — not just the congested one.

---

## Target architecture

```
ingest handler
    → per-type worker queue (capacity = workerQueueSize)
        → worker goroutine
            → writer
```

The ingest handler looks up the type's `TypeDescriptor` directly via the atomic routing table (`LookupType()`), then does a non-blocking send into the worker's queue itself. If the send fails, the item goes to `rejected` immediately with the specific type name and reason — before the HTTP response is sent.

---

## Changes required

### `pipeline/dispatcher.go`
- Remove `inputQueue` global channel.
- Remove `Start()` function (which created the channel and launched the dispatcher goroutine).
- Remove `Enqueue()` function.
- Keep `routing`, `LookupType()`, `AddItemType()`, `RemoveItemType()`, and `Reset()`.

### `pipeline/types.go` (or `config.go`)
- `CentralQueueSize` remains in `pipeline.Config` for now (accepted by the configure route, stored, but not used). Will be removed in a follow-up once the refactor is confirmed working.

### `server/server.go` (`handlePipelineStart`)
- Remove the call to `pipeline.Start(ctx, &s.wg, s.config.CentralQueueSize)`.
- The dispatcher goroutine no longer exists; `s.wg` tracks only the per-type worker goroutines started in `startType()`.

### `server/ingest_routes.go` (`handleIngest`)
- Replace the `pipeline.Enqueue()` call with:
  1. `pipeline.LookupType(itemType)` — if nil, add to rejected as unknown type (already done before this point via `ConvertItem`).
  2. Non-blocking send into `desc.Queue` (the worker's channel).
  3. If the send fails: add to rejected with `"typeName": "...", "error": "worker queue full"`.
  4. If the send succeeds: `ingested++`.

### Tests
- All existing tests remain valid. `centralQueueSize` is still sent in configure requests and accepted — it is just ignored internally.
- The rejected response for a full queue now says `"worker queue full"` instead of `"pipeline queue full"` — any tests asserting on that string need updating (currently none assert on the exact message).

---

## What stays the same
- All route handlers, route paths, and HTTP contracts.
- Per-type worker goroutines (`pipeline.Worker[T]`).
- Writer implementations (`FileWriter`, `BlobWriter`).
- Hot-add / hot-remove definition logic.
- All 35+ existing tests should continue to pass without modification.

---

## Follow-up (after confirmation)
- Remove `CentralQueueSize` from `pipeline.Config`, the configure route handler, and test helper `configureFileMode`.
- Update `docs/pipeline_architecture.md` to reflect the simplified design.
