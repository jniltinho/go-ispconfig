## 1. Second database handle

- [ ] 1.1 Add `[database] master_dsn` to the config struct, `config.toml.example` and the defaults; empty means "this node is the master" and every existing single-server path stays byte-identical.
- [ ] 1.2 Open the master handle in `cmd/daemon.go` and `cmd/serve.go` when `master_dsn` is set; expose `IsMaster()` / `MasterDB()` on the daemon so callers never guess which handle to use.
- [ ] 1.3 Add the startup log line naming the resolved role (master or slave, and against which host), so a misconfigured node is obvious in `journalctl` rather than by its silence.
- [ ] 1.4 Tests: a node with no `master_dsn` behaves exactly as today; a node with one reports slave and holds two handles.

## 2. Datalog pull with local replication

- [ ] 2.1 Read `sys_datalog` from the master handle when one is configured, keeping the existing `(server_id = ? OR server_id = 0)` filter and per-row cursor advance.
- [ ] 2.2 Replicate each row into the local database before dispatch (`REPLACE INTO` on insert/update, `DELETE FROM` on delete), so a slave's local mirror is what its own plugins read — port of `modules.inc.php:150-193`.
- [ ] 2.3 Write the cursor back to `server.updated` on **both** handles, master first, so a crash between the two re-processes rather than skips.
- [ ] 2.4 Quarantine path unchanged: a row whose payload is not valid JSON advances the cursor and logs, on both handles.
- [ ] 2.5 Integration test against two MariaDB containers: a row written on the master reaches the slave's local mirror exactly once, and the cursors converge.
- [ ] 2.6 Failure test: the master handle going away mid-batch leaves the cursor where the last fully-applied row was, and the next cycle resumes there.

## 3. Per-node database account

- [ ] 3.1 Add `goispsrv<N>` account provisioning to the installer: one least-privilege master-DB user per node, named after its `server_id` (port of `ispcsrv<N>`).
- [ ] 3.2 Derive the grant set from what a slave daemon actually issues — read on the tables it consumes, write on `server.updated` and the tables its role owns — and keep it in one place so it can be audited.
- [ ] 3.3 Refuse to provision an account for a `server_id` that does not exist or is not active on the master.
- [ ] 3.4 Document the grant set in `docs/multi-server.md`, replacing the current "every node uses the same `ispconfig` user" caveat.

## 4. Installer join mode

- [ ] 4.1 Add `install --join <master-url>` (or `--master-dsn`): register the node on the master (server row + its `server_ip` rows), mirror both locally, provision the account from block 3 and write a slave `config.toml`.
- [ ] 4.2 Make the join idempotent: re-running against an already-registered hostname reuses its `server_id` instead of creating a second row.
- [ ] 4.3 Ask for the roles at join time (`--web --mail --dns --db`), defaulting to none, so a node never silently claims a role it cannot serve.
- [ ] 4.4 Refuse the join when the master is unreachable, the credentials are wrong, or the schema version does not match — with the actual reason, not a generic failure.
- [ ] 4.5 Vagrant: add a second panel VM and a `make vagrant-cluster-up` target that stands up master + slave, so the flow is testable from a clean state.
- [ ] 4.6 Validate: join a fresh node, create a mail domain assigned to it on the master, confirm the node renders it and the master's cursor advances.

## 5. Mirror support

- [ ] 5.1 Widen the datalog filter so a mirror also consumes the rows of the server it mirrors.
- [ ] 5.2 Rewrite `server_id` on mirrored payloads before dispatch (port of `modules.inc.php:104-143`).
- [ ] 5.3 Add a `Mirrored` flag to the dispatched event so a plugin can suppress once-per-cluster side effects (DNS zone serial bumps, Let's Encrypt issuance, welcome mail).
- [ ] 5.4 Audit every plugin for side effects that must not run twice, and gate them on the flag.
- [ ] 5.5 Tests: a mirror renders the same config as its source and issues no duplicate external action.

## 6. server_ip_map

- [ ] 6.1 Add the `server_ip_map` entity (admin-only CRUD) over the existing model.
- [ ] 6.2 Apply the mapping when a mirror renders a vhost, so the mirrored config binds the mirror's own address instead of the source's.
- [ ] 6.3 Add the System → Server IPv4 mapping screen.

## 7. Documentation and closing the loop

- [ ] 7.1 Rewrite `docs/multi-server.md`: the manual four-step join becomes the fallback, `install --join` the documented path, and the "not supported yet" table shrinks to what is still true.
- [ ] 7.2 Update `docs/ARCHITECTURE.md` with the dual-handle flow and the replication step.
- [ ] 7.3 Note in `add-multiserver-mgmt`'s archive entry which capabilities were delivered earlier (`server-config-sync` at v0.4.0).
- [ ] 7.4 Full cluster validation on the Vagrant rig: master + one web slave + one mail slave, each applying only its own rows.
