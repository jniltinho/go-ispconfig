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

### Requirement: A request can be followed end to end

The panel SHALL correlate the datalog rows one API call produces under a single
request id, return it to the caller, and carry it on every event about those
rows. The id SHALL be stored in the existing `sys_datalog.session_id` column, so
correlation survives a restart and needs no schema change.

#### Scenario: One save journals several rows

- **WHEN** a single API call journals more than one datalog row
- **THEN** every row carries the same request id
- **AND** the response returns that id to the caller
- **AND** each event published about those rows carries it

#### Scenario: Part of a request fails

- **WHEN** a request journalled three rows and one of them is quarantined while
  the other two applied
- **THEN** the request reports a failed terminal state
- **AND** names the row that failed, rather than collapsing the outcome to a
  single success or failure

#### Scenario: The same request is submitted twice

- **WHEN** a save is double-clicked, or a POST is retried with the same request
  id
- **THEN** the rows are journalled once
- **AND** the second call is answered with the outcome of the first

#### Scenario: The panel restarts mid-flight

- **WHEN** serve restarts after journalling but before the daemon applies
- **THEN** the correlation is still readable from the database
- **AND** the rows apply normally

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

#### Scenario: The stream drops and reconnects

- **WHEN** the connection is lost and `EventSource` reconnects
- **THEN** the client refetches the rows it displays before trusting further
  events
- **AND** a change applied during the gap is reflected, even though no event for
  it was ever delivered

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
