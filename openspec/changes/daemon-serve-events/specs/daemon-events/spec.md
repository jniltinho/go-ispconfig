# daemon-events

## ADDED Requirements

### Requirement: The daemon publishes what it did

The config-apply daemon SHALL publish an event when a datalog row is applied or
quarantined, when it starts, on each tick, when it reloads a managed service and
when a scheduler job ends. Publishing SHALL NOT be able to fail or delay the
work it describes: with no reachable message bus the daemon behaves exactly as
it does today.

#### Scenario: A datalog row is applied

- **WHEN** the daemon finishes a datalog row cleanly and advances the cursor
- **THEN** a `datalog.applied` event carrying `server_id`, `datalog_id`,
  `dbtable` and `dbidx` is published
- **AND** the event is published from the same code path that advances the
  cursor, so it cannot report an outcome the database does not have

#### Scenario: A datalog row fails

- **WHEN** an apply fails and the row is quarantined
- **THEN** a `datalog.failed` event carrying the same identifiers plus `error`
  is published
- **AND** the `sys_log` row and `sys_datalog.error` are written as before

#### Scenario: The message bus is unreachable

- **WHEN** Redis is unconfigured, down, or rejects the publish
- **THEN** the apply completes normally and the daemon logs the drop
- **AND** no apply is retried, delayed or failed because of it

#### Scenario: The daemon announces it is alive

- **WHEN** the daemon boots, and on every tick thereafter
- **THEN** `daemon.started` and `daemon.heartbeat` are published with the
  version and the loaded module list

### Requirement: Serve streams events to the browser

The panel SHALL expose an authenticated Server-Sent Events endpoint that
forwards daemon events to connected browsers, filtered by what the session is
permitted to read.

#### Scenario: An operator watches a change apply

- **WHEN** a session with permission to read a record is connected to
  `GET /api/events` and that record's datalog row is applied
- **THEN** the corresponding event reaches that browser
- **AND** the list row's pending state clears without a manual page reload

#### Scenario: A session may not read the record

- **WHEN** an event names a record outside the session's permission scope
- **THEN** the event is not written to that stream

#### Scenario: A consumer stops reading

- **WHEN** a connected client stops draining its stream
- **THEN** its buffer is bounded and the connection is closed rather than
  allowed to grow the process

#### Scenario: The stream is unavailable

- **WHEN** the browser cannot open or keep an `EventSource`
- **THEN** the panel behaves as it does today — state resolves on reload — and
  no error is surfaced to the user

### Requirement: Health reports daemon liveness, not staleness

The health check SHALL report the daemon as alive based on its most recent
heartbeat when one has been observed, falling back to the datalog backlog
heuristic when it has not.

#### Scenario: The daemon dies on an idle panel

- **WHEN** the daemon stops and no writes occur
- **THEN** `/api/health?full=1` reports the daemon as degraded once the
  heartbeat interval is exceeded, without waiting for a write to age past the
  backlog grace period
