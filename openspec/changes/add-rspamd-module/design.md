# Design: Rspamd configuration, learning and spam-filter panel

## Context

`add-mail-module` ported `server/plugins-available/rspamd_plugin.inc.php` in full: `internal/mail/rspamd.go` writes one settings conf per identity under `/etc/rspamd/local.d/users/`, `internal/mail/wblist.go` writes one conf per `spamfilter_wblist` / `mail_access` row, and `serverUpdate` re-renders `dkim_signing.conf`, `options.inc`, `redis.conf` and `classifier-bayes.conf` from `[mail]` getconf plus `server_ip`. `internal/api/mailspamfilter.go` exposes CRUD for the four tables; `frontend/src/router.ts` mounts generic list/form routes for them.

The port is faithful — and that is the problem. The PHP plugin is only half of ISPConfig's Rspamd story. The other half is `install/lib/installer_base.lib.php::configure_rspamd()` (lines 2078–2250), which creates `/etc/rspamd/local.d/`, `local.d/maps.d/`, `override.d/`, and deploys the static baseline the plugin's output depends on:

- `local.d/users.conf` — the `settings { … }` block whose `.include(try=true; glob=true) "$LOCAL_CONFDIR/local.d/users/*.conf"` is the *only* thing that pulls in the per-identity confs the daemon writes;
- `local.d/{groups,antivirus,mx_check,milter_headers,neural,neural_group,arc}.conf`;
- `override.d/{rbl_group,surbl_group}.conf`;
- `local.d/maps.d/{dkim,dmarc,spf_dkim,spf}_whitelist.inc.ispc`;
- `local.d/worker-controller.inc` with `count`, a `rspamadm pw`-hashed `password` and `secure_ip = 127.0.0.1 / ::1`;
- the `greylist.conf` → `greylist.old` rename (ISPConfig stopped using the greylist module config and drives greylisting per identity instead);
- `chmod a+r,-x+X` on everything, `chgrp _rspamd` + `chmod 640` on `redis.conf`, `classifier-bayes.conf`, `worker-controller.inc`.

go-ispconfig has no rspamd installer step (`internal/installer/` = acme, bind, cert, ftp, mariadb, nginx, powerdns, serverip, systemd). So none of that exists on a go-ispconfig box, and the plugin's entire output is dead weight: `local.d/users/*.conf` is never included by anything.

Two further things ISPConfig never had at all, verified by grep over the whole PHP tree: `rspamc` (no ham/spam training anywhere — the Bayes classifier is configured and never fed) and any propagation of a `spamfilter_policy` edit (the shipped spec records this as expected behaviour: policy changes only reach Rspamd when some dependent row is touched).

Finally, the panel: `interface/web/mail/mail_{white,black}list_edit.php` and `spamfilter_{white,black}list_edit.php` carry per-request authorization (non-admins limited to `sender`/`recipient`, `source` must be an address inside a `mail_domain` the client owns), client and reseller limits (`limit_mail_wblist`, `limit_spamfilter_wblist`, `limit_spamfilter_user`, `limit_spamfilter_policy`) and a `server_id` derivation from the referenced `spamfilter_users` row. `internal/clients/limits.go` has no rule for any of them and `internal/api/mailspamfilter.go` has no `Prepare` hook, so a client can currently create an unlimited number of rows, on any `server_id`, of type `client` (IP/hostname scope), for domains they do not own.

## Goals / Non-Goals

**Goals:**
- Make the shipped `mail-rspamd-spamfilter` output effective: deploy the baseline snippets the per-identity and wblist confs depend on, from the daemon, idempotently, without clobbering operator edits.
- Feed the Bayes classifier: automatic training on Junk moves via Dovecot IMAPSieve, plus an on-demand action for backfilling an existing folder.
- Give `spamfilter_policy` edits a propagation path (`rspamd_resync`) instead of requiring every dependent row to be touched.
- Close the authorization/limit gap on `mail_access` and `spamfilter_*` writes with PHP-equivalent checks.
- A panel for spam filtering that matches the PHP information architecture: separate whitelist and blacklist screens per scope, real dropdowns, range-validated thresholds, learn/resync actions.

**Non-Goals:**
- Re-porting `rspamd_plugin.inc.php` — it is already ported and its per-row behaviour is unchanged here.
- DKIM/ARC signing (`mail-dkim`), per-user sieve / autoresponders / `mail_content_filter` (`mail-mailbox-management`), amavis/SpamAssassin, Rspamd WebUI proxying, Bayes cluster or Redis tuning, ClamAV provisioning.
- Schema changes; new installer steps; new external dependencies beyond `rspamc` (and optional `rspamadm`) already shipped with Rspamd.
- Translations beyond English.

## Decisions

### D1 — Baseline snippets ship as embedded assets deployed by `serverUpdate`, not by a new installer step

`RspamdPlugin.serverUpdate` already runs on `server_insert|update` and `server_ip_*`, already loads `.master` assets through `mastertpl.Load` (embedded, with `conf-custom` override), already writes into `/etc/rspamd/local.d/` and already guards on `isDir(RspamdDir)`. Extending its file list is a strictly smaller change than a new `internal/installer` step, and it self-heals: an operator who removes a snippet gets it back on the next server event, and existing boxes are fixed without re-running the installer.

Three deployment classes, driven by one table in the plugin:

| Class | Files | Policy |
|---|---|---|
| Templated | `dkim_signing.conf`, `options.inc`, `redis.conf`, `classifier-bayes.conf` | rewritten every run (**today's behaviour, unchanged**) |
| Daemon-owned static | `users.conf` | rewritten every run — it is the include glue; an edited copy silently disables all per-identity settings |
| Baseline static | `groups.conf`, `antivirus.conf`, `mx_check.conf`, `milter_headers.conf`, `neural.conf`, `neural_group.conf`, `arc.conf`, `override.d/{rbl_group,surbl_group}.conf`, `maps.d/{dkim,dmarc,spf_dkim,spf}_whitelist.inc.ispc` | write-if-absent |

Write-if-absent for the baseline class is deliberate: those files are exactly the ones an operator tunes (RBL groups, milter headers, neural weights), and ISPConfig itself only ever writes them once, at install. `users.conf` is the exception because it is structure, not policy.

Directories are created with `0755`, files `0644`, then `chgrp _rspamd` + `chmod 640` on `redis.conf`, `classifier-bayes.conf` and `worker-controller.inc` — reusing the group probe already in `serverUpdate` (`_rspamd`, falling back to `rspamd`). The `greylist.conf` → `greylist.old` rename runs once, guarded on the source existing.

### D2 — Controller worker: password optional, localhost always

`rspamc learn_spam` talks to the controller worker. `install/tpl/rspamd_worker-controller.inc.master` sets `count`, `password`, `secure_ip = 127.0.0.1` and `::1`, and ISPConfig hashes the password with `rspamadm pw` before writing it, generating one into `server.ini` when absent.

go-ispconfig does not write `server.ini` from the daemon and this change is not the place to add that machinery. So: `[mail] rspamd_password` (already typed in `internal/getconf`, `RspamdPassword`) is rendered when set — hashed through `rspamadm pw` when that binary exists, verbatim otherwise — and the `password` line is omitted when unset. `secure_ip` is always localhost, which is what makes `rspamc` from the daemon work in either case. `worker-controller.inc` is daemon-owned (rewritten every run) because it carries a credential that must track getconf.

`rspamc` invocations use `-h 127.0.0.1:11334` and pass `-P <password>` only when configured. When the controller is unreachable the action reports `StateError` with the `rspamc` stderr — never a silent success.

### D3 — Continuous learning through Dovecot IMAPSieve, not through Go

The upstream-recommended way to train Rspamd is Dovecot's `imap_sieve` plugin: moving a message into Junk runs a sieve script that pipes it to `rspamc learn_spam`, moving it out runs `learn_ham`. That is rung 4 of the ladder — the platform already does the work; Go only has to write four small files and never sits in the mail path:

- `/etc/dovecot/conf.d/90-sieve-imapsieve.conf` — `mail_plugins = $mail_plugins imap_sieve` for the imap protocol block, the two `imapsieve_mailbox*` rules (Junk as `to` → report-spam, Junk as `from` → report-ham), `sieve_pipe_bin_dir`, `sieve_global_extensions = +vnd.dovecot.pipe +vnd.dovecot.environment`;
- `<sieve dir>/report-spam.sieve` and `report-ham.sieve` — `pipe :copy "learn-spam.sh"` / `"learn-ham.sh"`, compiled with `sievec` when the binary exists;
- `<sieve_pipe_bin_dir>/learn-{spam,ham}.sh` — two-line wrappers calling `rspamc` with the configured host and optional password, mode `0755`.

This is the only Dovecot surface this change owns; per-user sieve, autoresponders and move-junk rules stay with the maildeliver plugin (`mail-mailbox-management`). The files are written by the same `serverUpdate` handler, write-if-absent for the sieve scripts (operators customise them), daemon-owned for the drop-in, and skipped entirely when `/etc/dovecot` is absent. A `dovecot` delayed reload is requested only when a file actually changed.

Trade-off accepted: this trains a **global** Bayes classifier, not per-user Bayes. Per-user Bayes needs `users_enabled` in `classifier-bayes.conf` and a much larger Redis footprint — explicitly a non-goal.

### D4 — On-demand learning and resync reuse `sys_remoteaction`, not a new transport

The foundation already dispatches `sys_remoteaction` rows through `engine.ActionFunc` (`internal/engine/daemon.go`, `registry.go`, states `ok` / `warning` / `error`), and the API already writes such rows for OS updates. Two new action types, both registered by the rspamd plugin, are the entire mechanism — no queue, no RPC, no new table:

- `rspamd_learn`, param `spam|ham:<email>[:<folder>]` — resolves `mail_user.maildir` for `<email>` on this server, defaults `<folder>` to `.Junk` for spam and `INBOX` for ham, and runs `rspamc learn_spam|learn_ham` over the message files in `cur/` and `new/`. Batched (files chunked per invocation) and capped; the cap is a constant, not a config knob, until someone hits it. `warning` when some messages fail (`rspamc` returns per-file errors for already-learned mail), `error` when the controller is unreachable.
- `rspamd_resync`, no param — re-renders every settings conf (all `spamfilter_users`, `mail_user`, `mail_forwarding` rows for this `server_id`) and every wblist conf (`spamfilter_wblist` + `mail_access`), removes orphaned `*.conf` in `local.d/users/` that no longer correspond to a row, then reloads. Implemented as a loop over the **existing** `userSettings` / `wblistUpdate` entry points with synthesised `engine.Data` — no second renderer, so there is no way for resync output to drift from event output.

`rspamd_resync` is also the answer to the shipped spec's "policy update alone does not write rspamd files": the policy form gets a resync button instead of a hidden fan-out on `spamfilter_policy` writes. A fan-out hook was rejected — a policy attached to a large domain would rewrite thousands of files inside the API request path, and PHP has no such hook either.

### D5 — Authorization and limits go in the existing hooks, not in new middleware

Everything needed is already in the foundation:

- `internal/clients/limits.go` gets four entries in its rule table — `access` → `limit_mail_wblist`, `wblists` → `limit_spamfilter_wblist`, `users` → `limit_spamfilter_user`, `policies` → `limit_spamfilter_policy` — each reusing `countByGroup` (the same counter `internal/clients/usage.go` already uses for the dashlet, so the panel's "used/limit" numbers and the veto agree by construction). Reseller limits come free via `checkResellerLimit`. Admin bypass, `<0 = unlimited`, `0 = veto` semantics are inherited.
- `Entity.Prepare` on `mail_access` ports `mail_whitelist_edit.php::onSubmit`: strip a leading `@` from `source`; for non-admins reject `type = client`, require `source` to parse as an email address and its domain to resolve to a `mail_domain` readable under the caller's riud scope. The `(server_id, source, type)` uniqueness the table already declares is surfaced as a validation error rather than a 500.
- `Entity.Prepare` on `spamfilter_wblist` ports `spamfilter_whitelist_edit.php::onSubmit`: `server_id` is **overwritten** from the `spamfilter_users` row referenced by `rid` (and the reference must be readable by the caller), so a client cannot aim a wblist row at another server.
- `spamfilterPolicyEntity()` drops `AdminOnly: true`. PHP grants clients their own policies under `limit_spamfilter_policy` and `{AUTHSQL}`; the riud scope plus the new limit rule reproduce that, and admin-only was a divergence introduced for lack of a limit rule.
- The `type` option list on `mail_access` is filtered to `sender`/`recipient` in the metadata response for non-admins (PHP does the same at the bottom of `form/mail_blacklist.tform.php`), so the UI never offers a value the API will reject.

### D6 — Panel: four screens where PHP has four, dropdowns where PHP has datasources

The generic `EntityForm` cannot express what the tform datasources express (`SELECT id,email FROM spamfilter_users WHERE {AUTHSQL}`), so these four surfaces get dedicated Vue views under `frontend/src/views/mail/`, in the same shape as the existing `MailboxForm.vue` / `DomainForm.vue`:

- **Spamfilter whitelist** and **Spamfilter blacklist** — two routes over `spamfilter_wblist`, list filtered on `wb`, form with `wb` fixed and hidden, `rid` as a dropdown of the caller's spamfilter users, priority as the PHP 1–10 select, no `server_id` field at all (derived, per D5).
- **Access whitelist** and **Access blacklist** — two routes over `mail_access`, filtered/fixed on `access` (`OK` vs `REJECT`, PHP's defaults), server dropdown, `type` limited to what the caller may use.
- **Policies** — thresholds (`rspamd_spam_tag_level`, `rspamd_spam_kill_level`, `rspamd_spam_greylisting_level`) as numeric inputs validated to a sane range with the plugin's own fallbacks (`6`, `15`, `0.1`) as placeholders, `rspamd_spam_tag_method` select, `rspamd_greylisting` checkbox with helper text stating the inheritance rule the plugin implements (an explicitly greylisted mailbox or forwarding wins over the policy; otherwise the policy value applies), and a **Resync** button (admin) posting `rspamd_resync`.
- **Learn** — a button on the mailbox form ("train Junk folder as spam") posting `rspamd_learn`, plus the same action available on the spamfilter users screen. Both render the resulting `sys_remoteaction` state.

Navigation entries are gated on the matching limit being non-zero, mirroring `lib/module.conf.php`.

### D7 — REST shape for the two actions

Two endpoints, both thin writers over `sys_remoteaction` with riud checks, swaggo-annotated like the rest of `internal/api`:

- `POST /api/mail/spamfilter/learn` — body `{server_id, email, kind: "spam"|"ham", folder?}`; the caller must have read access to the `mail_user` row; enqueues `rspamd_learn`; responds with the action id so the UI can poll its state.
- `POST /api/mail/spamfilter/resync` — body `{server_id}`; admin only; enqueues `rspamd_resync`.

No synchronous variant. Learning walks a maildir and shells out; doing it inside an HTTP request would tie the panel's request timeout to mailbox size for no benefit, and the daemon already owns every other shell-out in the system.

### D8 — Tests

- Golden-file test for `worker-controller.inc` (with and without password) and for the IMAPSieve drop-in, in the style of the existing `internal/mail/golden/` wblist vectors.
- Baseline deployment test with a temp `RspamdDir`: fresh dir gets the full set; a second run does not modify a hand-edited baseline file; `users.conf` **is** restored after being edited; `greylist.conf` is renamed once.
- `rspamd_resync` test asserting the file set after resync equals the file set produced by replaying the equivalent events one by one (the renderer-drift guard D4 relies on), plus orphan removal.
- `rspamd_learn` test with the fake command runner asserting the `rspamc` argv (host, optional `-P`, `learn_spam`, chunking) and the state mapping.
- Limit/authorization table tests for the four entities: over-limit veto, reseller veto, non-admin `type=client` rejection, foreign-domain `source` rejection, `server_id` derivation from `rid`.

## Risks / Trade-offs

- **[Deploying `users.conf` silently switches on enforcement]** → on an upgraded box, thresholds and greylisting that were configured in the panel but never applied start applying at the next server event. Mitigation: the change docs state it explicitly, `rspamd_resync` gives a deterministic point to apply it, and rendered levels come from the same policies the panel has been showing all along.
- **[Write-if-absent hides drift]** → a baseline file damaged by a third party is never repaired. Accepted: repairing it would overwrite legitimate operator tuning, which is worse. `rspamd_resync` does not touch the baseline class either; removing the file is the documented way to get it back.
- **[IMAPSieve requires Dovecot's sieve plugins]** → on a box without `dovecot-sieve`/`imap_sieve`, writing the drop-in would break Dovecot startup. Mitigation: deploy only when `/etc/dovecot/conf.d` exists **and** the sieve plugin is present; validate with `doveconf -n` before requesting a reload, and skip (log, no error) otherwise.
- **[`rspamc` batch over a huge Junk folder]** → the on-demand learn action is capped and chunked; it runs on the daemon cycle, not in the request path, and reports `warning` when it stops at the cap.
- **[Controller password in a world-readable file]** → `worker-controller.inc` is `chgrp _rspamd` + `chmod 640` like the other credential-bearing snippets; the password is hashed with `rspamadm pw` when available; it is never logged and never returned by the API.
- **[Broadening `spamfilter_policy` from admin-only to limit-gated]** → a client with `limit_spamfilter_policy > 0` can now create policies, matching PHP. Existing installs have that limit at its default, so nothing widens unless an admin sets it.

## Migration Plan

1. Land the embedded baseline assets and the extended `serverUpdate` file set; on existing installs the next `server_update` deploys them (idempotent, no service interruption beyond a delayed `rspamd` reload).
2. Land the controller worker config and the two remote actions; learning stays inert until an admin triggers it or IMAPSieve is deployed.
3. Land the limits and `Prepare` hooks. Pre-existing over-limit rows are untouched — the hooks only veto **creates**, exactly as `checkClientLimit` does in PHP.
4. Land the panel screens and nav; the generic routes for `wblists` / `access` are replaced, not duplicated.
5. Document in the mail module docs: the file inventory and who owns each file, the greylisting inheritance rule, and how to trigger learn/resync.

## Open Questions

- Should `rspamd_resync` be exposed per server (admin) only, or also to clients scoped to their own rows? Starting admin-only; the per-row events already cover the client case.
- Is a scheduled resync (e.g. nightly) worth it as a self-healing net, or is the manual action plus per-row events enough? Deferred until someone reports drift.
