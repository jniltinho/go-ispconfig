# Proposal: add-rspamd-module

> Follow-up to `add-mail-module` (archived, merged). Read "Why" first: the Rspamd **plugin** already shipped — this change makes it actually take effect and gives it a panel.

## Why

`add-mail-module` shipped the capability `mail-rspamd-spamfilter`, a behaviour-faithful port of `server/plugins-available/rspamd_plugin.inc.php`. What exists today in the tree:

- `internal/mail/rspamd.go` — per-identity settings files under `/etc/rspamd/local.d/users/` (IDN-encoded, `@`→`_`), domain fan-out to child mailboxes/forwardings, policy resolution from `spamfilter_policy`, greylisting resolution, and `server_update` regeneration of the four **templated** snippets `dkim_signing.conf`, `options.inc`, `redis.conf`, `classifier-bayes.conf` with `chgrp _rspamd` + `chmod 640` on the password-bearing ones.
- `internal/mail/wblist.go` — `spamfilter_wblist_*` / `mail_access_*` → `spamfilter_wblist_<id>.conf` / `global_wblist_<access_id>.conf`, priority +40/+30, address normalisation, delayed `rspamd` reload.
- `internal/mastertpl/templates/rspamd_{users.inc.conf,wblist.inc.conf,options.inc,redis.conf,dkim_signing.conf,classifier-bayes.conf}.master` — embedded, with `conf-custom` override support.
- `internal/api/mailspamfilter.go` + `internal/model/spamfilter.go` — CRUD for `mail_access`, `spamfilter_policy`, `spamfilter_users`, `spamfilter_wblist` with datalog state decorators.
- `frontend/src/router.ts` — generic `EntityList`/`EntityForm` routes for all four tables.
- Golden-file tests (`internal/mail/golden/generate_wblist.php`, `internal/mail/rspamd_test.go`, `wblist_test.go`).

Four gaps remain, found by reading the PHP tree against the Go tree:

1. **Everything the plugin writes is inert.** The include that makes `local.d/users/*.conf` load lives in `local.d/users.conf` (`install/tpl/rspamd_users.conf.master`: `settings { … .include(try=true; glob=true) "$LOCAL_CONFDIR/local.d/users/*.conf" }`). In ISPConfig that file — and `groups.conf`, `antivirus.conf`, `mx_check.conf`, `milter_headers.conf`, `neural.conf`, `neural_group.conf`, `arc.conf`, `override.d/{rbl_group,surbl_group}.conf`, `local.d/maps.d/*.ispc`, `worker-controller.inc` — is deployed by `install/lib/installer_base.lib.php::configure_rspamd()` (lines 2078–2250), not by the plugin. go-ispconfig's installer has **no mail/rspamd step at all** (`internal/installer/` ships bind, nginx, ftp, powerdns, mariadb, acme, cert, systemd steps only), so on a go-ispconfig box `/etc/rspamd/local.d/users.conf` never exists and every per-identity settings file and wblist conf the daemon writes is read by nobody. Spam thresholds, per-mailbox greylisting and white/blacklists are all silently no-ops.
2. **Nothing ever trains the Bayes classifier.** `rspamc` appears nowhere in `base/ispconfig3_install/` (verified: no `rspamc`, no `learn_spam`, no `imapsieve` anywhere in the PHP tree) and nowhere in `internal/`. `classifier-bayes.conf` is deployed and the redis backend configured, but no ham/spam ever reaches it — neither on demand nor when a user drags a message into Junk.
3. **The white/blacklist API is missing the PHP authorization and limit checks.** `mail_whitelist_edit.php` / `mail_blacklist_edit.php` restrict non-admins to `type ∈ {recipient, sender}`, require `source` to be a valid email address in a `mail_domain` the client owns, strip a leading `@`, and enforce `limit_mail_wblist` (client and reseller). `spamfilter_whitelist_edit.php` / `spamfilter_blacklist_edit.php` enforce `limit_spamfilter_wblist` and **derive `server_id` from the referenced `spamfilter_users` row**. `spamfilter_users_edit.php` and `spamfilter_policy_edit.php` enforce `limit_spamfilter_user` / `limit_spamfilter_policy`. In Go, `internal/clients/limits.go` has no rule for any of these four entities (they are only *counted* for the dashlet in `internal/clients/usage.go`), `mailAccessEntity()` offers the admin-only `client` type to every caller with no domain-ownership check, and `spamfilterWblistEntity()` accepts a caller-supplied `server_id`.
4. **The panel is raw table CRUD.** `policy_id`, `rid` and `server_id` are numeric text inputs where PHP renders SQL-backed `SELECT`s (`form/spamfilter_whitelist.tform.php` datasources); PHP has four separate nav entries (Whitelist / Blacklist for both scopes, `lib/module.conf.php` lines 61–128, each gated on the client limit) where Go has one combined list per table; and `spamfilterPolicyEntity()` is `AdminOnly: true` although PHP grants clients their own policies under `limit_spamfilter_policy`.

Reference PHP sources for this change (read-only under `base/ispconfig3_install/`):
- `install/lib/installer_base.lib.php::configure_rspamd()` (2078–2250) — baseline `local.d` / `override.d` / `maps.d` set, `worker-controller.inc` + `rspamadm pw`, `greylist.conf` → `greylist.old`, permissions.
- `install/tpl/rspamd_{users,groups,antivirus,mx_check,milter_headers,neural,neural_group,arc,whitelist,rbl_group,surbl_group,worker-controller.inc}.conf.master` and `install/tpl/rspamd_*_whitelist.inc.ispc.master` — the static snippets.
- `server/plugins-available/rspamd_plugin.inc.php` — already ported; extended here (`server_update` file set).
- `interface/web/mail/mail_{white,black}list_edit.php`, `spamfilter_{white,black}list_edit.php`, `spamfilter_users_edit.php`, `spamfilter_policy_edit.php`, `form/mail_blacklist.tform.php`, `form/spamfilter_{whitelist,users,policy}.tform.php`, `lib/module.conf.php` — panel authorization, limits, datasources, nav.

## What Changes

- **Baseline Rspamd provisioning moves into the daemon**: the rspamd plugin's `server_update` handler, which already writes the four templated snippets, also deploys the static baseline (`users.conf` — daemon-owned, the rest write-if-absent so operator edits survive), `override.d/{rbl_group,surbl_group}.conf`, `local.d/maps.d/*.ispc`, and performs the `greylist.conf` → `greylist.old` rename. No new installer step and no package management: the files are embedded `.master` assets with the existing `conf-custom` override path.
- **Controller worker config for `rspamc`**: `local.d/worker-controller.inc` from `rspamd_worker-controller.inc.master` with `secure_ip = 127.0.0.1 / ::1`, `password` only when `[mail] rspamd_password` is set (hashed with `rspamadm pw` when the binary exists), `chgrp _rspamd` + `chmod 640`.
- **Spam learning**: (a) continuous — the plugin writes a Dovecot IMAPSieve drop-in plus two sieve scripts and their `rspamc learn_spam` / `learn_ham` wrappers, so moving mail into or out of Junk trains Bayes with no panel interaction; (b) on demand — a `rspamd_learn` `sys_remoteaction` (existing foundation mechanism, `engine.ActionFunc` + `sys_remoteaction`) that trains an existing folder of an existing mailbox, exposed as `POST /api/mail/spamfilter/learn` and a button in the panel, for backfilling a Junk folder that predates the feature.
- **Spamfilter resync**: a `rspamd_resync` remote action that re-renders every settings and wblist conf for the server from the DB and reloads — the missing propagation path for `spamfilter_policy` edits (the shipped spec explicitly records that a policy edit alone changes nothing until a dependent row is touched) and the recovery path after the baseline lands on an existing box.
- **Authorization and limits parity** on the four spamfilter/access entities: `limit_mail_wblist`, `limit_spamfilter_wblist`, `limit_spamfilter_user`, `limit_spamfilter_policy` rules added to `internal/clients/limits.go` (client + reseller, reusing `countByGroup`); `Prepare` hooks porting the non-admin `type`/ownership checks and the `@`-strip on `mail_access`, and the `server_id`-from-`rid` derivation on `spamfilter_wblist`; `spamfilter_policy` stops being `AdminOnly` and is limit-gated instead.
- **Panel screens**: dedicated Vue views replacing the generic form for the four white/black surfaces (separate Whitelist and Blacklist entries as in PHP, `wb` / `access` prefilled and hidden), real dropdowns for server / policy / spamfilter user, a policy form with range-validated spam score thresholds, greylisting shown with its policy-inheritance semantics, and Learn / Resync actions. Nav entries gated on the same limits PHP gates on.

## Capabilities

### New Capabilities

- `rspamd-config`: daemon-owned Rspamd configuration beyond the per-row confs — baseline `local.d` / `override.d` / `maps.d` snippet deployment, controller worker config, Dovecot IMAPSieve learning hooks, and the `rspamd_learn` / `rspamd_resync` remote actions.
- `rspamd-ui-actions`: the panel and REST surface for spam filtering — separate white/blacklist screens for both scopes, spam score threshold forms, greylisting toggles, learn and resync actions, limit-gated navigation.

### Modified Capabilities

- `mail-rspamd-spamfilter`: two requirements change. "Server-level Rspamd config refresh" now covers the full baseline file set instead of only the four templated snippets. "mail_access management" and the spamfilter CRUD requirement gain the non-admin authorization, client/reseller limits and `server_id` derivation that `interface/web/mail/*_edit.php` enforce and the Go entities currently skip. Everything else in that capability (per-user settings files, wblist maps, golden-file fidelity) is unchanged and is **not** re-proposed here.

## Impact

- **Depends on `add-mail-module`** (mail module, rspamd plugin, mail getconf, spamfilter models/entities) and on the foundation (`port-ispconfig3-to-go`: `sys_remoteaction` dispatch, `engine.ActionFunc`, `mastertpl` embed + `conf-custom` override, `api.LimitHook`, riud scopes).
- Touched Go packages: `internal/mail` (rspamd plugin extension, new learn/resync actions), `internal/mastertpl/templates` (≈15 new embedded `.master` assets), `internal/api` (entity `Prepare` hooks, two action endpoints), `internal/clients/limits.go` (four rules), `frontend/src/views/mail/*` + `router.ts` + `locales/en.json`.
- DB: **no schema changes**. `spamfilter_policy`, `spamfilter_users`, `spamfilter_wblist`, `mail_access` and `sys_remoteaction` are used as they exist.
- External: `rspamd` (with `rspamc` and, optionally, `rspamadm`) and Dovecot with the `sieve` / `imap_sieve` plugins on the mail server. Every handler stays a no-op when `/etc/rspamd` is absent (PHP parity), so non-mail and non-Rspamd servers are unaffected.
- Operational: on an existing box the first `server_update` after this change deploys `users.conf`, which switches on per-user settings that were previously ignored — spam thresholds and greylisting start being enforced. This is the intended fix; the change docs must call it out as a behaviour change, not a silent one.

## Non-goals

- **DKIM/ARC signing rework** — `mail-dkim` owns key generation, `dkim_signing.conf`, `dkim_domains.map` / `dkim_selectors.map` and DNS publication. This change deploys the stock `arc.conf` baseline and touches nothing else there.
- **A full mail-filtering rewrite** — per-user sieve, autoresponders, move-to-Junk rules and `mail_content_filter` stay with `mail-mailbox-management` / the maildeliver plugin. The only Dovecot files this change owns are the IMAPSieve drop-in and its two learn scripts.
- **amavis / SpamAssassin** — superseded by Rspamd, as in `add-mail-module`.
- **Proxying or embedding the Rspamd web UI** in the panel. The controller worker is configured so `rspamc` works from localhost; the WebUI stays reachable only the way Rspamd itself exposes it.
- **Bayes cluster and Redis tuning** — `redis.conf` / `classifier-bayes.conf` keep taking their values from `[mail]` getconf exactly as they do today; no sharding, replication, per-user Bayes or neural-network tuning.
- **Antivirus (ClamAV) provisioning** — the stock `antivirus.conf` snippet is deployed for parity, but installing or configuring ClamAV is out of scope.
- Translations beyond English.
