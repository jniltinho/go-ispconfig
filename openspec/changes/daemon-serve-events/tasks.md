## 1. The event type and the bus

- [ ] 1.1 `internal/events`: an `Event{Type, ServerID, At, Data map[string]any}` and a `Publisher`/`Subscriber` pair over Redis pub/sub on channel `goisp:events`, reusing `config.QueueConfig` so there is no second Redis setting.
- [ ] 1.2 A no-op publisher when Redis is unconfigured or unreachable, chosen once at construction. Publishing must never block an apply or fail one: fire-and-forget with a bounded buffer, dropping under backpressure and logging the drop count. Test both paths.
- [ ] 1.3 Subscriber reconnects with backoff; a resubscribe never loses the process. Test with a Redis stopped and restarted under `internal/queue/redistest.go`.

## 1b. Request correlation

- [ ] 1b.1 Serve stamps `sys_datalog.session_id` with the request id — the client's `Idempotency-Key` when present (1b.6), otherwise one it mints — for every row a single API call journals, and returns it as `request_id` in the response. No schema change: the column exists (`internal/model/sys.go:71`) and this port writes nothing into it today.
- [ ] 1b.2 Note the divergence from the legacy, which puts the browser session there for `dataloghistory_undo` (`db_mysql.inc.php:762`). Per-request is finer-grained; if undo is ever ported it groups by user + time window instead. Record it in the migration doc.
- [ ] 1b.3 Every event the daemon publishes about a datalog row carries that `request_id` when the row has one. Test: one API call journalling three rows produces three events sharing one id.
- [ ] 1b.4 Migration adding `KEY (session_id)` to `sys_datalog` — the table ships only `PRIMARY KEY (datalog_id)` and `KEY (server_id, status)` (`internal/database/ispconfig3.sql:1681`), so correlating without it full-scans an append-only table. A migration, **not** an edit to the vendored schema.
- [ ] 1b.5 `GET /api/requests/<id>` (D3a): join `sys_datalog` to `server` on `server_id` and derive the state per D3a's four rules — grouping by `status` alone loses the server and each row must be compared against *its own* cursor. A row below the cursor still `pending` is `failed`, never `ok`. `404` for an id with no `sys_request` row. Tests: partial failure reports `failed` and names the row; rows behind two different cursors; a below-cursor `pending` row is not reported as success.
- [ ] 1b.6 Idempotency (D3b) on a **client-supplied** `Idempotency-Key` header, enforced by a `sys_request` row with `UNIQUE(request_id)` inserted inside the existing `datalogTxn` (`internal/api/dnsrr.go:38`) — a SELECT-then-INSERT check is a race both simultaneous submits win, so the constraint has to do the refusing. The id is returned only after that transaction commits. Validate a client key as a UUID: `session_id` already carries legacy PHP session values (`db_mysql.inc.php:762`) and a collision would return another request's rows. Tests: two concurrent submits with one key journal once; same key twice answers with the first outcome; no key twice journals twice; a crash mid-transaction leaves neither marker nor rows.
- [ ] 1b.7 `running` must expire (D3a): a request whose owning server has not sent a `daemon.heartbeat` within the interval reports `stale`, so a dead daemon or a decommissioned server does not leave it in flight for ever. Depends on phase 2.

## 2. The daemon publishes

- [ ] 2.1 `datalog.applied` / `datalog.failed` at the two points that already write the outcome (`internal/engine/daemon.go`: cursor advance and the quarantine path that writes `sys_log`). Same call site as the existing write, so the event cannot disagree with the database.
- [ ] 2.2 `daemon.started` on boot and `daemon.heartbeat` per tick, carrying version and the loaded module list.
- [ ] 2.3 `service.reloaded` from the shared reload helper, once, rather than in each plugin.
- [ ] 2.4 `job.finished` / `job.failed` from the scheduler wrapper.
- [ ] 2.5 `cert.*` from the ACME path — depends on `acme-as-go`; wire it there, not here.

## 3. Serve streams to the browser

- [ ] 3.1 `GET /api/events` (SSE), modelled on `migrationProgressHandler`: same headers including `X-Accel-Buffering: no`, same flush discipline, plus a **numbered** keepalive frame, so a client can spot a gap in the sequence (D6).
- [ ] 3.1b A `resync` event to every open stream whenever serve (re)subscribes to Redis (D6): a broker failover loses events without dropping the browser's connection, so `open` never fires and the client would never refetch on its own.
- [ ] 3.2 Permission filter before write: an event naming a record the session cannot read is not sent. Test with a client-scoped user and an admin against the same event.
- [ ] 3.3 A bounded per-connection buffer, written by the fan-out rather than the socket: one slow consumer must not stall delivery to the others (head-of-line). A full buffer disconnects that client alone. Test with two subscribers where one never reads and assert the other keeps receiving.
- [ ] 3.3b Disable the server write timeout on this route only, or every stream dies at the global deadline. Assert a stream survives past it.
- [ ] 3.4 Cap concurrent streams per session so a tab-hoarding browser cannot exhaust the process.

## 4. The UI reacts

- [ ] 4.1 A store subscribing once for the whole app, not per view — one `EventSource` per tab. Refetch the displayed rows on `open`, on `resync`, and on a missed keepalive (D6): pub/sub does not retain, so anything published during a gap is gone and the stream must never be treated as complete.
- [ ] 4.2 Lists carrying `_datalog_state` refresh the affected row on `datalog.applied` / `datalog.failed` instead of waiting for a manual reload. A form that just saved matches on the `request_id` it got back, so it can report *its own* change rather than any change to that record.
- [ ] 4.3 A toast on `datalog.failed` and `cert.failed`, using the components added in the dialogs/toasts change.
- [ ] 4.4 Fallback when `EventSource` fails: the existing behaviour, unchanged, with no error surfaced to the user.

## 5. Health

- [ ] 5.1 `checkDaemon` prefers the last `daemon.heartbeat` when one has been seen, falling back to the backlog heuristic. A dead daemon must be reported as dead on an idle panel, which today it is not.
- [ ] 5.2 Swagger for `/api/events` and the health change (mandatory per AGENTS.md).

## 6. Validation

- [ ] 6.1 Integration test: write through the API, assert the event arrives on a subscriber, assert the record's `_datalog_state` clears.
- [ ] 6.2 Redis-down test: every write still applies, the UI still resolves state on reload, nothing errors.
- [ ] 6.3 Lab pass on `192.168.56.12` with two browsers open.
- [ ] 6.4 Cross-review with qwen3.8-max before the PR.
