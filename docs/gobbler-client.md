# Gobbler Client SDK — Design Notes

## Purpose

A Go client library for sending log items to a Gobbler server. Analogous to the
Application Insights SDK for Go: the caller instruments their code by calling
`Log(typeName, fields)`, and the client handles batching, flushing, and HTTP
transport to the Gobbler ingest endpoint.

Intended uses:
- Instrumenting any application that has a Gobbler server as its log sink
- Gobbler monitoring itself ("Gobbler monitoring Gobbler") — a running Gobbler
  instance uses the client to emit its own operational metrics to a second
  Gobbler instance (or to itself)

## Module location

Option A (chosen): a subdirectory of this repo with its own `go.mod`.

```
gobbler/
  go.mod                    (github.com/kozwoj/gobbler)
  ...
  client/
    go.mod                  (github.com/kozwoj/gobbler-client)
    client.go
    options.go
```

During development the server repo can use a `replace` directive to reference
the client locally. The `client/` directory can be extracted to a standalone
repo later without changing its module path or public API.

## Type awareness

The client is semi-typed: it does not know item field schemas, but it does know
which type names are valid for the target server. Type names are registered at
construction time. Calling `Log` with an unregistered type name returns an error
immediately, before any network round-trip — useful for catching instrumentation
mistakes early during development.

Full schema awareness (field-level validation client-side) is a possible future
extension via `WithDefinitions([]ItemDefinition{...})`, but is not part of the
initial design.

## API sketch

```go
// Construction
client, err := gobblerclient.New("http://localhost:8080",
    gobblerclient.WithTypes("vm-shutdown", "vm-reboot", "allscalars"),
    gobblerclient.WithBatchSize(50),
    gobblerclient.WithFlushInterval(5 * time.Second),
)

// Logging — non-blocking; appends to the shared buffer
err = client.Log("vm-shutdown", map[string]any{
    "vmId":           "vm-123",
    "shutdownStart":  "2026-04-27 10:00:00",
    "shutdownReason": "OS update",
})

// Explicit flush — sends all buffered items to the server now
err = client.Flush()

// Graceful shutdown — flushes then stops the background goroutine
err = client.Close()
```

## Wire format

Each `Log` call contributes one entry to the shared buffer. On flush the buffer
is serialised as a JSON array and POSTed to `/gobbler/ingest`. For example, two
calls:

```go
client.Log("vm-shutdown", map[string]any{"vmId": "vm-123", "shutdownStart": "2026-04-27 10:00:00", "shutdownReason": "OS update"})
client.Log("vm-reboot",   map[string]any{"vmId": "vm-123", "eventTime": "2026-04-27 10:05:00", "rebootReason": "OS update"})
```

produce the following request body:

```json
[
  { "vm-shutdown": { "vmId": "vm-123", "shutdownStart": "2026-04-27 10:00:00", "shutdownReason": "OS update" } },
  { "vm-reboot":   { "vmId": "vm-123", "eventTime": "2026-04-27 10:05:00", "rebootReason": "OS update" } }
]
```

Items of different types are interleaved naturally in the array — the server
handles mixed-type batches in a single request.

## Batching and flushing

Items accumulate in a single shared in-memory buffer, each tagged with their
type name. This matches the Gobbler ingest body format directly:
`[{"typeName": {fields}}, ...]`. A flush is triggered by:
- the buffer reaching `batchSize` items total (threshold flush)
- the background goroutine's `flushInterval` ticker (time-based flush)
- an explicit call to `Flush()` or `Close()`

On flush the client POSTs the entire buffer to `/gobbler/ingest` in a single
request. Items of different types are naturally interleaved in the array, exactly
as the server expects.

## Error handling

The client distinguishes server responses by their HTTP status class:

- **400 Bad Request** -- the request body was structurally invalid (not a JSON
  array, or malformed JSON). This is a client-side bug; retrying the same payload
  will always get 400. No retry.
- **200 OK with rejected items** -- the server processed the batch but rejected
  individual items (unknown type name, invalid field values). The items were
  consumed; retrying them would produce the same rejection. No retry. The
  rejected list is surfaced as an error to the caller.
- **5xx Server Error** -- the server was unavailable or failed internally. The
  batch may not have been processed at all, so retry is appropriate. The buffer
  is held (not drained) until a successful 2xx response is received.

Log returns an error synchronously if the type name is not registered.
Network errors and non-2xx responses are returned from Flush and Close. When
a threshold flush occurs inside Log, any resulting error is also returned to
the caller.

## Null-object / no-op behaviour

New() always returns a usable Client value even when construction fails. On
failure it returns a nopClient plus the error. The caller can log or ignore the
error and continue -- all calls on a nopClient are safe no-ops. This means
application code instrumented with the client needs no nil guards and can run
without a Gobbler server present.

gobblerclient.Nop() returns a no-op client directly, for use in tests or
environments where monitoring is explicitly disabled.

Client is defined as an interface so the no-op and real implementations are
interchangeable, and test code can substitute its own implementation.

## Server validation at construction and swap

New() and SwapServer(newURL) both validate the target server before
committing to it:

1. GET /gobbler/pipeline/status -- must return running: true
2. GET /gobbler/definition/list -- all type names registered with the client
   must be present

If validation fails, New() returns a nopClient plus an error. SwapServer
rejects the new target and returns an error; the client continues using the
current server unchanged.

## Server swap and migration

SwapServer(newURL) enables planned migration to a new server instance (e.g.
scheduled maintenance). It validates the new server (see above), then atomically
updates the endpoint. Any flush in progress against the old server completes
normally. All subsequent flushes -- including items already buffered -- go to the
new server. No items are lost.

## High availability

A backup / failover server is not a client-side concern. If high availability is
required, the operator runs two warm Gobbler instances behind a load balancer and
gives the client a single virtual IP or DNS name. The client sees one endpoint;
the load balancer handles failover transparently.

## Open questions

- Should Close drain remaining items even if a mid-flush error occurs, or stop
  on first error?
- Retry policy for 5xx: fixed delay, exponential backoff, max attempts -- not
  decided for v1.

## Server contract (Gobbler server requirements)

- POST /gobbler/ingest must return 400 when the request body cannot be
  parsed as a JSON array at all (currently returns 200 -- needs to be fixed).
- POST /gobbler/ingest returns 200 with a rejected array for
  item-level validation failures; this is not an error from the transport
  perspective.
- GET /gobbler/pipeline/status must include a running boolean field.
- GET /gobbler/definition/list must return the registered type names in a
  form the client can check against its own registered types.
