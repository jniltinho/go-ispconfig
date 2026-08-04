# Design: daemon-serve-events

## D1. What the legacy does, checked line by line

Worth reading first, because it sets the bar: there is no prior art to port
here. ISPConfig3 has **no** mechanism for telling the browser that a change was
applied.

| Step | Legacy | Reference |
|---|---|---|
| Panel journals a change | `datalogSave()` / `datalogInsert()` / `datalogUpdate()` write one `sys_datalog` row | `interface/lib/classes/db_mysql.inc.php:722,769,811` |
| Correlation | the row carries PHP's `session_id()` | `db_mysql.inc.php:762` |
| Daemon wake-up | **crontab, once a minute** — no queue, no signal | `install/lib/installer_base.lib.php:4147` (`* * * * * server.sh`) |
| Daemon consume | `SELECT * FROM sys_datalog WHERE datalog_id > ? AND (server_id = ? OR server_id = 0) ORDER BY datalog_id LIMIT 0,1000` | `server/lib/classes/modules.inc.php:105` |
| Cursor advance | `UPDATE server SET updated = ?` | `modules.inc.php:199` |
| Status shown to the operator | nothing per record. `sys_datalog` surfaces only as a raw journal under Monitor | `interface/web/monitor/datalog_list.php` |
| Apply errors | written to `sys_log`, read by opening Monitor → System Log | `interface/web/monitor/show_sys_state.php:607` |
| Push to browser | **none.** No `EventSource`, no WebSocket, no `text/event-stream`, no hidden iframe, no periodic AJAX anywhere in `interface/` | grep over `interface/` returns nothing |

So in the legacy an operator saves a site and waits up to 60 seconds with no
feedback, then reloads. This port is already ahead on both halves: the asynq
`datalog:ready` task replaces the minute cron, and `_datalog_state`
(`internal/api/sites.go:696`) gives per-record pending/error state the legacy
never had. What is missing is only the last hop — telling a page already open
that the state changed.

That matters for scope: there is no compatibility constraint here. Nothing in
the legacy schema, UI or API depends on how we do this.

## D2. Why the job-per-request design is not the one to build

The proposal on the table was: each panel operation becomes an asynq job with a
UUID, the daemon updates job state, and the UI reads `/api/jobs/<uuid>` through
the asynq Inspector. Three things break it, all checkable:

**The outcome would live somewhere that forgets it.** asynq deletes a completed
task immediately unless `asynq.Retention(...)` is set, and even with retention
the record is in Redis. Redis is deliberately optional in this port — a node
with no reachable Redis still applies every change through the `tick_seconds`
poll (`internal/queue/queue.go`, and `docs/multi-server.md` promises it). A
status endpoint backed by Redis returns 404 for work that in fact succeeded.
`sys_datalog` already holds `status` and `error` per row
(`internal/model/sys.go:69-70`), durably, in the database every node reads.

**One request is not one job.** Saving a site can journal several rows — the
vhost, its DNS records, its FTP user — and each row is addressed to a
`server_id`. On a multi-server install one panel action fans out to N nodes with
N independent outcomes. A single job id cannot carry that; it would need a
parent with children, which is a second scheduler on top of the one the datalog
already is.

**The Inspector is a debugging API.** It scans queue keys; it exists for
`asynqmon`, not for a status call on every list render.

The datalog *is* the job record. What it lacks is not durability or state — it
is a handle to say "these rows are my request".

## D3. The design: correlate, then stream

`sys_datalog.session_id` already exists (`internal/model/sys.go:71`) and this
port writes nothing into it. The legacy puts the browser session there, to group
an undo (`dataloghistory_undo.php`); we put a **per-request id**, which is
strictly finer-grained and costs no schema change.

```
  browser                serve                    Redis                daemon
     │                     │                        │                    │
     │ POST /api/sites ────▶                        │                    │
     │                     │ INSERT sys_datalog     │                    │
     │                     │   session_id = req-7f  │                    │
     │                     │ ─── datalog:ready ────▶│ queue server:<id> ─▶
     │ ◀── 201 {request_id:│ "req-7f"}              │                    │
     │                     │                        │        apply ──────┤
     │                     │                        │◀── PUBLISH ────────┤
     │                     │                        │   goisp:events     │
     │ ◀═══ SSE event ═════│◀── SUBSCRIBE ──────────│                    │
     │   datalog.applied   │                        │                    │
     │   request_id: req-7f│                        │                    │
```

- **serve → daemon** is unchanged: `sys_datalog` row plus the existing
  `datalog:ready` asynq task, tick-poll as fallback.
- **daemon → serve** is Redis pub/sub on `goisp:events`. go-redis already
  arrives with asynq, so no new module. Fire-and-forget: a failed publish never
  fails or delays an apply.
- **serve → browser** is SSE on `GET /api/events`, the pattern
  `migrationProgressHandler` (`internal/api/migration.go:592`) already proves,
  headers included (`X-Accel-Buffering: no` for the nginx in front).
- Every event carries `server_id`, the record identity, and `request_id` when
  the row had one — so a page can match an event to the save the user just made,
  which is the job-level tracking the proposal wanted.

**The database stays the source of truth.** The event is a notification with no
information the database does not already have. Drop every event and the panel
behaves exactly as it does today: `_datalog_state` still resolves on reload.
That is the property that lets Redis stay optional.

### D3a. The request-level object is a query, not a store

Refusing the asynq job did not refuse the requirement behind it, and pretending
otherwise would just push the aggregation into the browser — which cannot even
know how many rows its POST journalled. `GET /api/requests/<id>` exists, and it
is one query:

```sql
SELECT d.datalog_id, d.server_id, d.status, s.updated
FROM sys_datalog d JOIN server s ON s.server_id = d.server_id
WHERE d.session_id = ?
```

The join is not incidental. Grouping by `status` alone loses `server_id`, and
each row has to be compared against **its own** server's cursor — on a
multi-server install one request's rows sit behind different cursors. The rule,
in order:

1. any row with `datalog_id > s.updated` → `running`;
2. else any row with `status = 'error'` → `failed`;
3. else any row still `pending` → `failed`, **not** `ok`. A row below the cursor
   that never left `pending` means the daemon passed it without recording an
   outcome; calling that success is how a silent failure becomes invisible;
4. else `ok`.

That is what makes partial failure representable — 2 rows applied and 1 failed
is a real outcome a single job id could not express.

Two properties of the engine this rests on, both already true: a failed row is
quarantined with `status = 'error'` and the cursor **advances past it**
(`internal/engine/daemon.go:281`), so one bad row does not strand the rest of a
request in `pending`; and rows are processed in `datalog_id` order, which is the
dependency order a request needs (client before site before zone).

Rule 1 needs a way out or a request whose daemon died sits in `running`
for ever, indistinguishable from one in flight — the same for a row addressed to
a decommissioned server. The `daemon.heartbeat` of phase 2 is what closes it:
`running` with no heartbeat from the owning server for longer than the
heartbeat interval reports `stale`, which is a state an operator can act on.

This needs an index. `sys_datalog` ships `PRIMARY KEY (datalog_id)` and
`KEY (server_id, status)` and nothing on `session_id`
(`internal/database/ispconfig3.sql:1681`), so correlating without one full-scans
an append-only audit table that grows forever. `KEY (session_id)` goes in as a
migration, not an edit to the vendored schema: extra indexes do not break
compatibility with a legacy panel, changing the shipped DDL would.

### D3b. The request id is also the idempotency key

This is the property the asynq design had for free and that is worth keeping
deliberately: a task id deduplicates a retried enqueue. Here, a double-clicked
save or a retried POST journals the rows twice and the daemon applies them
twice. Today that is masked because most applies are convergent, but it is luck,
not design.

The id has to come **from the client** for this to work at all, and the two
roles must not be conflated:

- a **serve-minted** id correlates. It is unique per request by construction, so
  a double-clicked save produces two different ids and deduplicates nothing;
- a **client-supplied** `Idempotency-Key` header deduplicates, because the retry
  carries the same value the first attempt did.

So: mutating requests may send `Idempotency-Key`; serve uses it as the request
id and answers a repeat with the first outcome instead of journalling again.
Without the header serve mints an id, the response still carries it, and the
request is correlatable but not idempotent — which is today's behaviour, not a
regression.

The browser sends it on form saves, where the double-click is real. An API
client that wants exactly-once opts in the same way. Making serve mint it and
calling that idempotency would have been a comfortable lie.

**Checking is not deduplicating.** `session_id` cannot be `UNIQUE` — a request
is N rows by design — so "SELECT, and if absent INSERT" is a race two
simultaneous submits both win. The uniqueness has to live somewhere it can be
declared: a `sys_request` row keyed `UNIQUE(request_id)`, inserted **inside the
transaction that journals the datalog rows**. The second submit's insert fails
on the constraint, the transaction rolls back, and it is answered with the first
outcome. The database refuses the duplicate; no application-level check can.

That the journal is already transactional is what makes this cheap: handlers go
through `datalogTxn` (`internal/api/dnsrr.go:38`), so the marker joins an
existing transaction rather than introducing one. It also closes the partial
journal — a crash mid-request leaves neither the marker nor the rows, so the
retry writes the whole thing rather than completing someone's half-batch.

Two consequences worth stating:

- the id is returned to the client **after** that transaction commits. Before it,
  there is nothing to look up;
- `GET /api/requests/<id>` answers **404** for an id with no `sys_request` row.
  Without that, an unknown id and a committed-but-empty request look identical
  and a client polls a typo for ever.

Client-supplied keys must be namespaced — a UUID, validated — because
`session_id` already carries values from the legacy panel, which writes PHP's
`session_id()` there (`db_mysql.inc.php:762`). A collision would hand one
request's caller another's rows.

## D4. SSE, not WebSocket, not Inspector polling

One-way traffic, server to browser. SSE costs a handler and an `EventSource`,
reconnects on its own, and is already in the codebase with a polling fallback.
WebSocket adds a module, an upgrade handshake, ping/pong and a second auth path
to carry messages that never travel upward — reconsider it when something
genuinely bidirectional appears, not before. Short-polling the asynq Inspector
is worse than both: more requests, less information, and it disappears when
Redis restarts.

## D5. Phasing

1. `datalog.applied` / `datalog.failed` with `request_id`, the SSE endpoint, and
   list rows that clear their own badge. This is the complaint an operator
   actually has, and it is the whole of the value.
2. `daemon.started` / `daemon.heartbeat`, so the health check stops inferring
   liveness from cursor staleness — today it cannot tell a dead daemon from an
   idle one (`internal/api/health.go:157`).
3. `cert.*`, `service.reloaded`, `job.*` on the same channel. No redesign: they
   are more event types, not more machinery.

WebSocket is not phase 4. It is a different change, to be proposed when there is
a bidirectional use case.

## D6. A missed event must not be a wrong screen

Redis pub/sub does not retain: a subscriber that is disconnected when a message
is published never sees it. `EventSource` reconnects on its own, and the
existing migration stream handles no `Last-Event-ID`
(`internal/api/migration.go`), so a serve restart, a network blip or a
Redis failover drops whatever fired in the gap.

This is acceptable **only** because the event carries no state. The rule that
makes it safe: on `open` — first connect and every reconnect — the client
refetches the rows it is displaying, exactly as it does on page load. The stream
then adds latency improvements, never authority. A page that trusted the stream
to be complete would show a stale badge forever after one dropped message.

**That rule alone is not enough**, because the gap that matters most does not
drop the browser's connection. Redis fails over, or serve resubscribes, and
events are lost while the `EventSource` stays happily open — so `open` never
fires, the client never refetches, and the badge is wrong until someone reloads
by hand. A daemon that applies a row and dies before its `PUBLISH` does the same
thing with no outage at all.

Two mechanisms, both cheap:

- serve emits a `resync` event to every connected stream whenever *it*
  (re)subscribes to Redis. The clients refetch on `resync` exactly as they do on
  `open`, so a broker blip reaches them without a disconnect;
- a numbered keepalive comment every N seconds. A client that sees a gap in the
  sequence, or no keepalive within the window, refetches on its own. That covers
  the publish that never happened, which no server-side signal can announce.

Neither makes the stream authoritative. They just make "I may have missed
something" observable, which is the only property the refetch rule needs.

The alternative is a resumable log (`Last-Event-ID` against a Redis stream), and
it is not worth it here: the database already answers "what is the state now"
in one query, which is a cheaper and more honest recovery than replaying a
window of events.

## D7. What has to stay true

- **Permission filtering is the safety property of this change, not a detail.**
  The channel carries record identities from every client of the panel. An
  unfiltered stream is cross-tenant information disclosure — a reseller learning
  another reseller's domain names. The SSE handler applies the same riud check
  the REST list applies, before writing, and the test for it is not optional.
- An apply must never fail, retry or slow down because publishing failed.
- One slow reader must not stall the others: fan-out writes to per-connection
  buffers, never to sockets in a shared loop. A full buffer disconnects that
  client and nobody else.
- The SSE route needs the server write timeout disabled for it, or every stream
  dies at the global deadline.
- With Redis down, everything degrades to today's behaviour and nothing surfaces
  an error to the user.

## D8. Two gaps this change inherits and does not fix

Naming them so they are not mistaken for solved:

- **A row stuck in `pending`.** If the daemon dies mid-apply, the row stays
  above the cursor forever and the UI shows "pending" indefinitely. Nothing
  reaps or retries it today; the events make the symptom visible sooner, which
  is an argument for fixing it, not a fix.
- **No per-row retry policy.** asynq gives retry and timeout to the *wake-up
  task*, not to the datalog rows it wakes the daemon for. The operational
  visibility asynqmon would give over a job queue has no equivalent over the
  datalog.

Both belong to the datalog engine, not to the event channel.
