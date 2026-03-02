# RED TEAM REPORT: base/core Unhook (commit 4256313)

**Scope**: `luxfi/indexer@main` commit `4256313` — replace `hanzoai/base/core` with `chi/v5` + stdlib
**Date**: 2026-04-12
**Reviewer**: Red (adversarial)
**Target**: 10 files, 37 handlers converted, `explorer/` package

---

## Summary

7 findings: 0 critical, 1 high, 3 medium, 2 low, 1 info.

No SQL injection, no SSRF, no stop-ship. One high-severity issue (GraphQL
mutation-over-GET) requires a one-line fix. Three medium issues affect API
compatibility and operational safety.

---

## Findings

### [HIGH] H-01: GraphQL HandleFunc Accepts All HTTP Methods (Mutation-Over-GET / CSRF)

**Description**: `RegisterRoutes` uses `r.HandleFunc("/graphql", ...)` and
`r.HandleFunc("/local/graphql", ...)`. Chi's `HandleFunc` accepts every HTTP
method (GET, POST, PUT, DELETE, PATCH, OPTIONS). The handler branches on
`r.Method == http.MethodGet` vs everything-else. Any non-GET method (PUT,
DELETE, PATCH) proxies to the G-Chain endpoint or executes local GraphQL,
identical to POST.

This means a mutation can be triggered via GET-with-body on some HTTP clients,
or via PUT/DELETE which some CORS preflight policies treat differently.

The old code used `v2.Any(...)` which is semantically identical, so this is not
a regression from the commit itself. However, the refactor was an opportunity to
tighten this and it was missed.

**Location**: `explorer/explorer.go:219-220`
**Attack Complexity**: Low
**Exploitability**: Craft a cross-origin PUT request to `/v1/explorer/graphql`
with a GraphQL mutation body. If CORS allows PUT but not POST (misconfigured
upstream proxy), the mutation executes. Also, some WAFs/CDNs treat GET+body as
passthrough.
**Impact**: Mutation execution via unexpected HTTP methods. The federated
endpoint proxies to G-Chain, so any G-Chain mutation schema is exposed.
**Detectability**: Not caught by current tests (tests only exercise GET and POST).
**Fix Hint**: Replace `r.HandleFunc` with explicit `r.Get` + `r.Post` for both
GraphQL routes. Return 405 for other methods.

---

### [MEDIUM] M-01: Error Response Shape Change — Missing `status` Field

**Description**: The old `base/core` `ApiError` serialized as:
```json
{"status": 404, "message": "Block not found.", "data": {}}
```
The new `notFoundError` / `badRequestError` helpers serialize as:
```json
{"message": "block not found", "data": {}}
```

Two breaking differences:
1. The `status` field is absent from the new response body.
2. The message is not sentence-cased (no capital, no trailing period). The old
   code used `inflector.Sentenize()` which capitalizes and appends a period.

Any frontend that reads `response.body.status` to determine the error type
(rather than the HTTP status code) will break.

**Location**: `explorer/explorer.go:258-271`
**Attack Complexity**: N/A (compatibility issue)
**Impact**: Frontend breakage for error handling code that parses the JSON body.
**Fix Hint**: Add `"status": code` to the error map. Capitalize and period-terminate
the message string to match old behavior, or document the shape change and
coordinate with frontend teams.

---

### [MEDIUM] M-02: No Panic Recovery Middleware

**Description**: The old `base/core` router included built-in panic recovery.
The new `RegisterRoutes` creates a bare `chi.NewRouter()` with no middleware.
If any handler panics (nil pointer dereference in `scanMaps`, unexpected type
assertion in `formatBlock`, etc.), the entire goroutine crashes and the HTTP
connection drops without a response.

The outer mux (in `cmd/explorer/main.go`) also uses `http.NewServeMux()` with
no recovery middleware. A single panic in any handler kills the connection and
may leave the client hanging.

**Location**: `explorer/explorer.go:169`
**Attack Complexity**: Medium — requires triggering a code path that panics
**Impact**: DoS via connection drop. Repeated panics degrade service for all
clients on the same listener.
**Detectability**: Would show as a goroutine crash in logs but no structured
error response.
**Fix Hint**: Add `r.Use(middleware.Recoverer)` as the first middleware in
`RegisterRoutes`, or apply it at the outer mux level.

---

### [MEDIUM] M-03: No Graceful Drain for Service.Close() In-Flight Requests

**Description**: `Service.Close()` stops the notification worker and closes DB
connections immediately. If called while HTTP handlers are mid-flight (e.g.,
during a `queryTxListCtx` with a 5-second context timeout), the DB handle
becomes invalid and queries fail with `sql: database is closed`.

The `cmd/explorer/main.go` calls `srv.Shutdown(context.Background())` which
drains HTTP connections, but `Service.Close()` is not coordinated with this.
If the shutdown sequence is: (1) cancel context, (2) `srv.Shutdown()`, (3)
`Service.Close()` -- that's correct. But `Service.Close()` is not documented
as requiring this order, and the standalone server mode doesn't call it at all
(the indexer goroutines just exit on context cancellation).

**Location**: `explorer/explorer.go:108-114`
**Attack Complexity**: Low (race during shutdown)
**Impact**: Request errors during shutdown window. Not exploitable by an
external attacker, but affects reliability.
**Fix Hint**: Document the shutdown ordering requirement. Optionally, add a
`sync.WaitGroup` to `Service` that handlers increment/decrement, and have
`Close()` wait for it to drain before closing DB connections.

---

### [LOW] L-01: Chi URLParam Passes NUL Bytes to hexToBytes

**Description**: Chi URL-decodes `%00` to a literal NUL byte (0x00) before
returning it from `chi.URLParam()`. This NUL byte reaches `hexToBytes()` which
calls `hex.DecodeString()`. The hex decoder silently truncates at the invalid
byte, returning a partial decode.

The truncated byte slice is then used as a parameterized SQL query argument.
This is safe (no injection possible) but produces incorrect query results --
the query matches a shorter prefix instead of the intended address.

Example: `/addresses/0xdeadbeef%00pwned` decodes to `URLParam = "0xdeadbeef\x00pwned"`,
`hexToBytes` returns `[]byte{0xde, 0xad, 0xbe, 0xef}` (truncated at NUL).
The query `WHERE hash = ?` matches `0xdeadbeef` instead of returning 404.

**Location**: `explorer/handlers.go` (all `chi.URLParam` call sites),
`explorer/format.go:67-71`
**Attack Complexity**: Low
**Impact**: Incorrect query results. An attacker could discover address prefixes
by probing with NUL-terminated strings. Not a data leak (the address data is
public) but violates the principle of exact-match lookups.
**Fix Hint**: Validate `chi.URLParam` results against `hexAddrPattern` or
`hexHashPattern` regex before passing to `hexToBytes`. Return 400 for invalid
format. The standalone server already does this (`isValidHexAddr`).

---

### [LOW] L-02: Deleted collections.go — Watchlist/Tags/Custom-ABI Endpoints Gone

**Description**: `collections.go` was deleted. It defined four Base collections:
`explorer_watchlist`, `explorer_address_tags`, `explorer_tx_tags`,
`explorer_custom_abis`. These were user-facing features that allowed registered
users to:
- Maintain a watchlist of addresses
- Tag addresses and transactions
- Upload custom ABIs for unverified contracts

The commit message says "not used by explorer; chain data read from indexer
SQLite, not Base DB". This is correct for chain data, but these collections
stored *user* data, not chain data. If any frontend calls these endpoints, those
features are now broken.

**Verification**: Grepped `luxfi/explore` (frontend repo not available locally).
The endpoints were Base-native CRUD on `/api/collections/explorer_watchlist/records`
etc., not on `/v1/explorer/*`. Since the explorer binary (`cmd/explorer/main.go`)
uses `StandaloneServer` (not the Service+chi path), these collections were only
available when the explorer ran inside a Base instance. The standalone binary
never had them.

**Location**: `explorer/collections.go` (deleted)
**Impact**: If any deployment ran the old Base+plugin mode and users created
watchlist/tag data, that data pathway is gone. Feature regression, not a
security issue.
**Fix Hint**: If watchlist/tags are needed, implement them as standalone endpoints
backed by SQLite (not Base collections). Otherwise, document the removal.

---

### [INFO] I-01: writeJSON Does Not Handle Encode Errors

**Description**: `writeJSON` calls `json.NewEncoder(w).Encode(v)` after
`w.WriteHeader(code)`. If `Encode` fails (e.g., the value contains a channel
or function — impossible in practice for the current handlers), the HTTP status
code is already written and cannot be changed. The error is silently discarded.

This is a theoretical concern. All current callers pass `map[string]any` or
struct values that are guaranteed JSON-serializable.

**Location**: `explorer/explorer.go:245-249`
**Impact**: None in practice.
**Fix Hint**: Log encode errors if you want defense-in-depth.

---

## Blue's Flagged Vectors — Disposition

### Vector 1: Chi path-param with 0x-prefixed hex + URL-special chars
**Status**: PARTIALLY CONFIRMED (L-01 above)
- `%2F` (slash): Chi does NOT decode it in path params. Returns literal `%2F`. **Safe**.
- `%23` (#): Chi decodes to `#`. `hexToBytes` truncates at `#`. Same prefix-match issue as NUL. **Low**.
- `%00` (NUL): Chi decodes to `\x00`. `hexToBytes` truncates. **Low**.
- SQL injection: Impossible. All values pass through `hexToBytes` → `[]byte` → parameterized query. No string interpolation of user input into SQL.

### Vector 2: HandleFunc method dispatch on GraphQL
**Status**: CONFIRMED (H-01 above)
- `HandleFunc` accepts all methods. PUT/DELETE/PATCH reach the handler.
- Handler treats all non-GET as "execute query" (same as POST).
- The old `v2.Any(...)` had the same behavior, so not a regression, but the refactor should have tightened it.

### Vector 3: Error JSON shape mismatch
**Status**: CONFIRMED (M-01 above)
- Old: `{"status": 404, "message": "Block not found.", "data": {}}`
- New: `{"message": "block not found", "data": {}}`
- Missing `status` field and different message casing.

### Vector 4: External callers of removed Register(core.App, Config)
**Status**: CLEAR
- Grepped entire `luxfi/indexer` repo: zero calls to `explorer.Register(` or `explorer.MustRegister(`.
- `cmd/explorer/main.go` uses `NewStandaloneServer`, not the Service path.
- The Service path is only used in tests. No external breakage.

### Vector 5: Deleted collections.go
**Status**: CONFIRMED LOW (L-02 above)
- Watchlist/tags/custom-ABIs were Base-native CRUD, not explorer API routes.
- Only available in Base+plugin mode (not standalone binary mode).
- Standalone binary (the production path) never had these features.
- If no Base+plugin deployment exists, this is a no-op deletion.

---

## Additional Checks — Disposition

### Middleware order
- **Recoverer**: ABSENT (M-02). No panic recovery in `RegisterRoutes` or the outer mux.
- **CORS**: Not handled in `RegisterRoutes`. Expected to be handled by the ingress layer (hanzoai/ingress). Acceptable if the deployment guarantees ingress-level CORS.
- **Auth**: The old Base plugin had Base's auth middleware. The new Service has none. This is intentional (the explorer serves public chain data), but if any handler needs auth in the future, it must be added explicitly.

### SSE handler / Flusher / goroutine leak
- **Not applicable to this commit**. The Service handlers do not include SSE. SSE/WebSocket is in `standalone.go` (the `realtimeHandler` method on `StandaloneServer`), which was not changed by this commit.

### Graceful shutdown
- `Service.Close()` exists but is not coordinated with HTTP drain (M-03).
- `cmd/explorer/main.go` has signal handling + `srv.Shutdown()` + context cancellation. Adequate for the standalone path.

### GraphQL query-depth/complexity limiting
- The old Base+plugin mode did not have query-depth limiting either. The local GraphQL handler is a stub (returns `__typename` only). The federated handler is a reverse proxy to G-Chain, which must enforce its own limits. No regression.

### Response Content-Type
- `writeJSON` explicitly sets `application/json`. **Correct**.
- `writeHTML` explicitly sets `text/html; charset=utf-8`. **Correct**.

### HTTP status codes
- `writeJSON` takes explicit code parameter. Callers pass correct codes. **Correct**.
- `notFoundError` returns 404, `badRequestError` returns 400. **Correct**.

### Build verification
- `go build ./explorer/` succeeds.
- `go vet ./explorer/` succeeds.
- Refactored handler tests pass (GraphQL, cross-chain search, blocks, transactions).
- Pre-existing standalone test failures (DEX routes, concentration endpoint) are not caused by this commit.

---

## Blue Handoff

### What Blue got right
- Clean 1:1 handler conversion. All 18 `PathValue` → `chi.URLParam` mappings verified.
- Parameterized SQL throughout. No string interpolation of user input into queries.
- `writeJSON` sets Content-Type explicitly. Good.
- LimitReader on webhook body parsing preserved from old code.
- SSRF protection in notification worker preserved (isInternalURL, redirect blocking, DNS rebinding re-check at delivery time).
- Service lifecycle (NewService/Close) is clean. Constructor does setup, Close does teardown.
- Test conversion from `newTestEvent` to `httptest.NewServer` is correct and more realistic.

### What Blue missed
- GraphQL `HandleFunc` should be `Get` + `Post` (H-01).
- Error response body shape changed (M-01) — `status` field dropped, message casing changed.
- No panic recovery middleware (M-02).
- No input validation on chi.URLParam results before hexToBytes (L-01) — standalone server validates, Service handlers do not.

### Fix priority for Blue
1. **H-01**: Replace `r.HandleFunc` with `r.Get` + `r.Post` for GraphQL routes (1 line each).
2. **M-01**: Add `"status": code` to error response maps, or document the breaking change.
3. **M-02**: Add `r.Use(middleware.Recoverer)` in `RegisterRoutes`.
4. **L-01**: Add hex validation to URLParam results (optional but recommended for parity with standalone).

### Re-review scope
After fixes: re-test GraphQL routes with PUT/DELETE to confirm 405, and verify error response JSON shape matches old format.

---

RED COMPLETE. Findings ready for Blue.
Total: 0 critical, 1 high, 3 medium, 2 low, 1 info
Top 3 for Blue to fix:
1. GraphQL HandleFunc accepts all methods (H-01)
2. Error response missing `status` field (M-01)
3. No panic recovery middleware (M-02)

Re-review needed: yes — GraphQL method restriction, error shape
Recommendation: fix-then-ship
