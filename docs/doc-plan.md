# Documentation Plan

This file tracks the documentation cleanup and completion work. Annotate sections with decisions, additional scope, or "skip" as needed.

---

## What we have after cleanup

| File | Role | State |
|---|---|---|
| `README.md` | Primary entry point for operators and contributors | Needs work — see below |
| `docs/REST-commands.md` | Authoritative REST API reference | **Complete** — all endpoints documented |
| `docs/pipeline_architecture.md` | Internal architecture reference for contributors | Keep as-is |
| `docs/JSON-schemas.md` | All JSON object definitions: configuration, item type, and self-logging item types | **Complete** |
| `docs/config_schema.json` | JSON schema for pipeline/configure body | Keep — referenced by REST-commands.md |
| `docs/item_schema.json` | JSON schema for definition/add body | Keep — referenced by REST-commands.md |
| `gobbler-client/README.md` | SDK entry point for application developers | **Missing — needs to be created** |

---

## Work items

### 1. `README.md` — rewrite top-to-bottom

The current README has a good overview and architecture narrative, but it:
- has no quick-start (a reader cannot get from zero to running without hunting through the REST reference)
- has no deployment / configuration section
- has no mention of the `gobbler-client` Go SDK
- ends with a vague "more details in docs/" rather than specific links

**Proposed structure:**

```
# Gobbler
  one-paragraph summary

## Quick start
  prereqs (Go 1.24+, local file mode)
  build / run
  minimal startup sequence (curl examples or .http file reference)

## Configuration
  pipeline/configure fields explained in prose (not just JSON)
  file mode vs blob mode
  self-logging setup (reference JSON-schemas.md for type definitions)

## Architecture
  keep current content; update "docs/" reference to specific links

## REST API reference
  one-line summary of each endpoint group; link to docs/REST-commands.md

## Go SDK
  one paragraph; link to gobbler-client repo / README

## Contributing
  build, test commands; link to docs/pipeline_architecture.md
```

<!-- ANNOTATIONS: add scope notes here -->

---

### 2. `gobbler-client/README.md` — create from scratch

Audience: application developers who want to integrate Gobbler logging into their Go service.

**Proposed structure:**

```
# gobbler-client
  one-paragraph summary

## Installation
  go get command; version note

## Quick start
  New() call with options
  Log() call
  Flush / Close with context

## Options reference
  WithTypes, WithBatchSize, WithFlushInterval, WithMaxBufferSize, WithHTTPClient
  NopClient for testing

## Error handling
  ErrBufferFull, ErrBufferFullServerDown
  context cancellation on Flush/Close

## Registering log types on Gobbler
  point to Gobbler README / REST-commands.md

## Versioning
  semver; link to releases
```

<!-- ANNOTATIONS: add scope notes here -->

---

### 3. `docs/gobbler-logging.md` — ~~decide destination~~

Merged into `docs/JSON-schemas.md` (section 3). File deleted.

---

### 4. `docs/REST-commands.md` — no work needed

All endpoints are documented. Only touch this file when a route changes.

---

### 5. `docs/pipeline_architecture.md` — no work needed

Contributor reference; keep as-is.

---

## Order of execution

1. ~~Decide destination for `gobbler-logging.md` content (item 3 above)~~ — done
2. Write `gobbler-client/README.md` (item 2)
3. Rewrite `README.md` (item 1) — do last so it can link to the finished gobbler-client README

---

## Out of scope (for now)

- Publishing to GitHub / removing the `replace` directive in `gobbler/go.mod`
- `godoc` / `pkg.go.dev` coverage
- Changelog / release notes
