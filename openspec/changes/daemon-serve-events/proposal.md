# Proposal: daemon-serve-events

## Why

The two processes talk in one direction only. `serve` tells `daemon` there is
work; `daemon` never tells `serve` anything.

```
                    ┌──────────────────────────────────────────────┐
                    │                  MariaDB                     │
   ┌────────┐       │  sys_datalog · server.updated · sys_log      │
   │browser │       └───────▲──────────────────────┬───────────────┘
   └───┬────┘               │ INSERT               │ SELECT
       │ HTTPS :8080        │                      │ (poll, on page load)
   ┌───▼──────────┐  datalog:ready   ┌─────────────┴────┐
   │ go-ispconfig │─────────────────▶│  go-ispconfig    │
   │    serve     │  asynq/Redis     │     daemon       │
   │              │  queue server:<id>│                 │
   └──────────────┘                  └──────┬───────────┘
          ▲                                 │ apply
          │                                 ▼
          └─── ✗ nothing ───────  nginx · bind · postfix · certs
```

Concretely, in today's tree:

- **serve → daemon** is `datalog.SetReadyNotifier` → `queue.EnqueueDatalogReady`
  → asynq task `datalog:ready` on queue `server:<id>`, deduplicated for 30s
  (`internal/queue/queue.go:84`). Redis down is survivable: the daemon's
  `tick_seconds` poll finds the same rows in `sys_datalog`.
- **daemon → serve** has no transport at all. What the daemon did is inferred
  by *reading the database*: `datalogStateDecorator`
  (`internal/api/sites.go:696`) compares each record's `datalog_id` against
  `server.updated` to synthesise `_datalog_state = pending`, and reads
  `sys_datalog.status`/`error` for `error`. `checkDaemon`
  (`internal/api/health.go:157`) does the same trick with a 60s grace to answer
  "is the daemon alive", so **serve never contacts the daemon** — it looks at
  how stale the cursor is.
- **serve → browser** has no push either, except one endpoint: the migration
  wizard streams SSE (`/api/system/migration/progress`, `internal/api/migration.go:592`)
  and the frontend consumes it with `EventSource` plus a polling fallback
  (`MigrationWizard.vue:257`). No other view refreshes itself; `_datalog_state`
  goes stale until the user reloads the page.

The visible consequences:

1. Save a site, watch the "pending" badge, reload by hand until it clears.
   Nothing tells the page the vhost was written.
2. A renewed certificate, a reloaded nginx, a scheduler job that failed at 02:00
   — all invisible until someone opens Monitor → System Log.
3. An apply that fails writes `sys_datalog.error` and a `sys_log` row, and the
   operator learns about it the next time they happen to look at that record.
4. A daemon that dies is only noticed by the backlog check, one minute late,
   and only if someone calls `/api/health?full=1`.

## What changes

A daemon → serve event channel, and a browser-facing stream over it.

- The daemon publishes to **Redis pub/sub** on `goisp:events` (go-redis is
  already an indirect dependency through asynq — no new module).
- Serve subscribes and fans out to connected browsers over **SSE**, the pattern
  the migration wizard already proves, with the same polling fallback.
- With Redis unreachable, everything degrades to today's behaviour: no push,
  the badges still resolve on reload. The database stays the source of truth;
  the channel is a notification, never a state carrier.

Events (payload is small and always carries `server_id`, so a multi-node panel
can attribute them):

| Event | Emitted when | Payload |
|---|---|---|
| `datalog.applied` | a datalog row finishes cleanly | `datalog_id`, `dbtable`, `dbidx` |
| `datalog.failed` | a row is quarantined | + `error` |
| `cert.issued` / `cert.renewed` | an ACME issuance succeeds | `domains`, `not_after` |
| `cert.failed` | issuance fails | `domains`, `error` |
| `service.reloaded` | a plugin reloads nginx/bind/postfix | `service` |
| `job.finished` / `job.failed` | a scheduler job ends | `job`, `duration`, `error` |
| `daemon.started` / `daemon.heartbeat` | on boot, then per tick | `version`, `modules` |

`daemon.heartbeat` is what turns the health check from "the cursor looks stale"
into "the daemon last spoke 3s ago" — the current check cannot tell a dead
daemon from an idle one until a write happens to age past the grace period.

## Why SSE and not WebSocket

The traffic is one-way, server to browser. SSE costs one handler and an
`EventSource`; the browser reconnects on its own; it survives the nginx in front
of the panel with the `X-Accel-Buffering: no` header already used by the
migration stream. WebSocket would add a dependency (`gorilla/websocket`), an
upgrade handshake, ping/pong keepalives and a second auth path, to carry
messages that never travel upward. Long-polling is strictly worse than both and
the codebase already rejected it once, keeping polling only as the SSE fallback.

## Open questions

1. **Scope of the first cut.** `datalog.*` alone already fixes the badge, which
   is the complaint an operator actually has. Certificates and jobs can follow
   on the same channel without redesign.
2. **Per-user filtering.** Events describe records a non-admin may not be
   allowed to see. Simplest correct answer: the SSE handler filters by the
   session's permissions before writing, rather than the daemon publishing
   per-user topics.
3. **Redis as a hard dependency.** Today a node can run with Redis unreachable.
   This must stay true — the channel degrades, it does not become required.

## Status

**Design proposal — awaiting approval before implementation.** No code written.
