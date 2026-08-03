# Design: Fail2ban module (jails, bans, panel UI)

## Context

Unlike every other module in this port, there is nothing to port. The research is summarised in the proposal; the load-bearing facts for design are:

1. **No plugin, no module, no master.** `server/plugins-available/` has no `fail2ban_plugin.inc.php`; `server/mods-available/` has no fail2ban module; `server/conf/` has no fail2ban master. `installer_base.lib.php:2701 configure_fail2ban()` is `// To Do`, and the Debian override at `debian60.lib.php:238` has its two `copy()` calls commented out. `install/install.php:566` prompts and calls the no-op.
2. **The only jail ISPConfig ever templated** is `install/tpl/dovecot_fail2ban_jail.local.master`, orphaned:
   ```ini
   [dovecot-pop3imap]
   enabled  = true
   filter   = dovecot-pop3imap
   action   = iptables-multiport[name=dovecot-pop3imap, port="pop3,pop3s,imap,imaps", protocol=tcp]
   logpath  = /var/log/mail.log
   maxretry = 20
   findtime = 1200
   bantime  = 1200
   ```
   with `install/tpl/dovecot-pop3imap.conf.master` as its filter. That file is the only concrete precedent for jail option values; everything else in the ISPConfig world comes from the Perfect Server tutorials.
3. **The panel's fail2ban "UI" is a log dump.** `cron.d/100-monitor_fail2ban.inc.php` (`*/5 * * * *`) tails `/var/log/fail2ban.log` — path resolved in `monitor_tools.inc.php:663-673`, identical for debian/redhat/suse/gentoo — into `monitor_data` type `log_fail2ban`, always `state = 'no_state'`. `tools_monitor.inc.php:474 showFail2ban()` `nl2br()`s it into `monitor/show_data.php?type=fail2ban`. No jails, no bans, no actions.
4. **Firewall is the analogue to copy.** `internal/firewall` is: `module.go` (one table hook → `firewall_insert/update/delete`), `plugin.go` (event subscriptions, `isLocal` server_id gate, `CommandRunner`), `ufw.go` (probe binary + version, diff old/new, apply, `EffectivePorts` lock-out guard), `ports.go` (pure functions with unit tests), `internal/api/firewall.go` (declarative admin-only `Entity` with a policy key), and a `System → Firewall` sidebar entry in `frontend/src/modules.ts`. This change reproduces that skeleton one-for-one.
5. **The panel already provisions the services to protect.** `internal/installer/ftpstep.go` installs `pure-ftpd-mysql`; nginx, MariaDB, Bind/PowerDNS and (with the mail module) Postfix/Dovecot come from the same pipeline. The jail catalogue can be gated on the same `server` row flags the modules use, so a DNS-only node never enables a dovecot jail.

## Goals / Non-Goals

**Goals:**
- Panel-managed fail2ban: an admin edits jails in the UI, the daemon renders `/etc/fail2ban/jail.local` + owned filters and reloads fail2ban — no SSH required.
- Ship a safe default jail set covering the services this stack provisions (ssh, postfix, postfix-sasl, dovecot, pure-ftpd, panel login) with defaults an operator can tune per jail.
- Live ban visibility and one-click unban from the panel, driven by `fail2ban-client`.
- Never lock the admin out: `ignoreip` guard mirroring the firewall module's protected-ports guard, with mandatory unit coverage.
- Zero DB schema change; zero effect on installs where fail2ban is absent (skip cleanly, exactly like the UFW probe).

**Non-Goals:**
- Everything in the proposal's Non-goals: iptables_plugin port, per-client jails, mail/recidive/blocklist actions, multi-server ban aggregation, CrowdSec, non-Debian-family distros, web-auth jails, translations.
- Parsing `/var/log/fail2ban.log` to build the ban list — `fail2ban-client` is the API (D5).
- Managing fail2ban's own `jail.conf`, `fail2ban.conf` or packaged `filter.d/*.conf` files. We own `jail.local` and `filter.d/goisp-*.conf` only.

## Decisions

### D1 — `internal/fail2ban`: one module, one plugin, one client
Package layout mirrors `internal/firewall` file-for-file:

| File | Mirrors | Responsibility |
|---|---|---|
| `module.go` | `firewall/module.go` | table hook → `fail2ban_insert/update/delete`; registers the `fail2ban` service for delayed reload |
| `plugin.go` | `firewall/plugin.go` | event subscriptions, `isLocal(server_id)` gate, `CommandRunner`, binary probe |
| `jails.go` | `firewall/ufw.go` | render `jail.local` + owned filters, `fail2ban-client -t` test, reload |
| `catalogue.go` | `firewall/ports.go` | pure jail-catalogue + `ignoreip` functions, fully unit-tested, no I/O |
| `client.go` | (new) | thin `fail2ban-client` wrapper: `status`, `status <jail>`, `set <jail> unbanip/banip`, output parsing |

*Alternative*: fold fail2ban into `internal/firewall` since both touch netfilter — rejected: different lifecycle (fail2ban owns its chains), different config surface, and it would make the firewall lock-out tests harder to reason about.

### D2 — Config lives in `server.config [fail2ban]`, no new table
ISPConfig3 has no fail2ban table and the proposal forbids schema changes. `server.config` is an INI blob already parsed by `internal/getconf` (`decodeSection(raw["web"], …)`, `raw["mail"]`, …), so a new `[fail2ban]` section costs nothing:

```ini
[fail2ban]
enabled     = y
ignoreip    = 127.0.0.1/8 ::1
bantime     = 1200
findtime    = 1200
maxretry    = 5
backend     = auto
jail_sshd        = y
jail_sshd_maxretry = 3
jail_postfix     = y
jail_postfix_sasl = y
jail_dovecot     = y
jail_pureftpd    = y
jail_goisp_panel = y
```

A typed `Fail2banConfig` struct with `ini:"…"` tags joins `WebConfig`/`MailConfig` in `internal/getconf`. Writes go through the existing `server` row update path, so a jail edit produces a normal `server_update` datalog row — which is also the event the plugin subscribes to (D3).

*Alternatives*: a new `fail2ban` table (rejected — schema change, and ISPConfig3 has no precedent to stay compatible with); a standalone TOML file (rejected — bypasses datalog, breaks multi-server, and the panel could not edit it through the entity framework).

### D3 — Events: `server_update` is the trigger, `fail2ban_*` are announced for symmetry
Because the config lives in `server.config`, the real trigger is the foundation's `server_update` event (the same one `rspamd_plugin` uses for server-level config in the mail design). The module still announces `fail2ban_insert/update/delete` and registers a hook so a future dedicated table, or a manual "apply now" queue job, can raise them without changing subscribers. The plugin subscribes to all four; `server_update` re-renders unconditionally, the `fail2ban_*` events re-render the named jail.

*Alternative*: subscribe only to `server_update` — rejected: leaves no event name for the panel's explicit "apply" button and the resync tool.

### D4 — Render `jail.local`, never `jail.conf`
On Debian/Ubuntu, `/etc/fail2ban/jail.conf` is package-owned and replaced on upgrade; `/etc/fail2ban/jail.local` overrides it and is the documented operator file (this is also exactly what ISPConfig's commented-out `debian60` code targeted). Rendering rules:

- Single generated file `/etc/fail2ban/jail.local` from an embedded `fail2ban_jail.local.master`, with a `# Managed by go-ispconfig — do not edit` header.
- Existing unmanaged `jail.local` is backed up to `jail.local~` on first write (same `writeFileBackup` helper `internal/installer/ftpstep.go` uses).
- `/etc/fail2ban/jail.d/*.conf` is **left alone** — an operator's own drop-ins keep working and win by fail2ban's own ordering.
- Owned filters are written only as `/etc/fail2ban/filter.d/goisp-*.conf`; packaged filters (`sshd`, `postfix`, `postfix-sasl`, `dovecot`, `pure-ftpd`) are referenced by name, never rewritten. This is the one place we deliberately diverge from ISPConfig's orphaned template, which shipped its own `dovecot-pop3imap` filter — upstream fail2ban has shipped a maintained `dovecot` filter for years.
- Apply order: write temp → `fail2ban-client -t` (config test) → move into place → delayed `fail2ban` service reload. A failing test aborts the write and logs, leaving the previous file intact.

### D5 — `fail2ban-client` is the live-state API, not the log file
Ban state is queried and mutated exclusively through argv invocations of `fail2ban-client` via the foundation `CommandRunner` (no shell strings, matching `firewall/ufw.go`):

| Operation | Command | Parsed into |
|---|---|---|
| jail list | `fail2ban-client status` | `Jail list:` CSV line |
| jail detail | `fail2ban-client status <jail>` | currently failed/total failed, currently banned/total banned, `Banned IP list:` space-separated |
| unban | `fail2ban-client set <jail> unbanip <ip>` | ok / "is not banned" |
| ban | `fail2ban-client set <jail> banip <ip>` | ok |
| reload | `fail2ban-client reload` (or `reload <jail>`) | ok |
| config test | `fail2ban-client -t` | exit code |

`<jail>` is never taken raw from the request: it must match a jail present in `fail2ban-client status` output (and `^[a-zA-Z0-9_-]{1,64}$`). `<ip>` must parse via `netip.ParseAddr`/`ParsePrefix`. This closes the argument-injection surface even though argv already prevents shell injection.

The `log_fail2ban` monitor collector (deferred by the monitor module) is filled in alongside, reusing `monitor.TailFile` with `/var/log/fail2ban.log` — same tail semantics as `monitor_tools.inc.php`, no new mechanism.

*Alternative*: parse `/var/log/fail2ban.log` for `Ban`/`Unban` lines to derive current state — rejected: lossy across restarts and log rotation, and there is a real API.

### D6 — Lock-out guard for `ignoreip`, mirroring the firewall guard
`internal/firewall` computes protected TCP ports (panel port + SSH port) and refuses to enable UFW without them; `firewall/lockout_test.go` makes that a CI invariant. Fail2ban gets the equivalent for source IPs:

- The effective `ignoreip` written to `jail.local` is always the configured value **unioned** with `127.0.0.1/8` and `::1`.
- When a ban/unban request arrives over HTTP, the caller's own client IP (resolved through the existing `trustedProxies` logic in `internal/api/api.go`) is refused as a ban target — the admin cannot ban themselves out of the panel.
- `POST /api/fail2ban/jails/{jail}/ban` refuses any IP inside the effective `ignoreip` set with a 422, since fail2ban would silently ignore it anyway.
- A `lockout_test.go` fixture asserts no rendered `jail.local` ever omits loopback from `ignoreip`.

`ignoreip` is advisory, not a firewall hole: it only stops fail2ban from banning, it does not open a port.

### D7 — Service gating: only render jails for services this server runs
A DNS-only node must not get a dovecot jail whose `logpath` does not exist (fail2ban refuses to start a jail with a missing logpath unless the file is created). Each catalogue entry declares a gate, evaluated at render time:

| Jail | Gate |
|---|---|
| `sshd` | always (every node has sshd) |
| `postfix`, `postfix-sasl`, `dovecot` | `server.mail_server = 1` **and** the mail module enabled |
| `pure-ftpd` | `server.web_server = 1` (the `ftpStep` gate — FTP ships with web) |
| `goisp-panel` | always |

Plus a belt-and-braces existence check on the resolved `logpath` (or a working `backend = systemd`); a gate that passes but whose log is absent downgrades the jail to `enabled = false` with a logged warning rather than breaking the whole config test.

### D8 — Log paths and backend per distro
Supported set is Debian 11–13 and Ubuntu 22.04/24.04 (`internal/installer/distro.go`). Two wrinkles:

- **sshd**: Debian 12+ and Ubuntu 24.04 may have no `/var/log/auth.log` when rsyslog is not installed. Resolution order: `backend = systemd` when `/var/log/auth.log` is absent and `systemd` python bindings are available (`python3-systemd`, pulled by the `fail2ban` recommends on these releases), else `logpath = /var/log/auth.log` with `backend = auto`. The installer step (D9) installs `python3-systemd` explicitly so the systemd path is always available.
- **mail**: `postfix`/`postfix-sasl`/`dovecot` use `/var/log/mail.log` — the same path ISPConfig's orphaned template hardcoded — falling back to `backend = systemd` under the same rule.

Resolution is a pure function in `catalogue.go` (`ResolveBackend(jail, fsProbe) (logpath, backend string)`), table-tested, with no live filesystem in tests.

### D9 — Installer step
New `internal/installer/fail2banstep.go`, same shape as `ftpStep`:

- Package `fail2ban` (+ `python3-systemd`) added to the profile set in `packages.go` — `packagesStep` already skips already-installed packages and does `systemctl enable --now`, so the step itself only seeds `[fail2ban]` defaults into `server.config` and writes the first `jail.local` through the same renderer the plugin uses (one code path, not two).
- `Skip("fail2ban disabled")` when the answer file opts out; the module and plugin then never load.
- Idempotent re-run: `writeFileBackup` reports `changed=false` and no reload is queued.

### D10 — REST API shape
Two surfaces under `/api/fail2ban`, both admin-only (`AdminOnly: true` + a `admin_allow_fail2ban_config` policy key, same as `firewallEntity()`'s `admin_allow_firewall_config`):

| Route | Purpose |
|---|---|
| `GET /api/fail2ban/config` / `PUT` | jail settings CRUD via the declarative `Entity` framework over the `[fail2ban]` getconf section (datalog-writing `server` row update) |
| `GET /api/fail2ban/jails` | live jail list with currently-banned / total-banned counts |
| `GET /api/fail2ban/jails/{jail}` | jail detail incl. banned IP list, failregex counts, effective options |
| `POST /api/fail2ban/jails/{jail}/unban` | body `{"ip":"1.2.3.4"}` → `set <jail> unbanip` |
| `POST /api/fail2ban/jails/{jail}/ban` | body `{"ip":"1.2.3.4"}` → `set <jail> banip`, subject to D6 |
| `POST /api/fail2ban/reload` | explicit apply (re-render + test + reload) |

Live-state routes mount alongside the monitor routes (`registerMonitorRoutes` pattern in `internal/api/api.go`) rather than through `registerEntities`, because they read a daemon, not a table. Every route carries swaggo annotations, `CookieAuth`/`BearerAuth` security and a `server_id` query parameter defaulting to the local server.

### D11 — UI shape
`frontend/src/views/system/Fail2banView.vue`, registered in `frontend/src/modules.ts` as `{ labelKey: 'sidebar.system.fail2ban', path: '/system/fail2ban', adminOnly: true }` directly under the existing `sidebar.system.firewall` entry:

- **Jails tab**: DataTable of jails — name, enabled, currently banned, total banned, `maxretry`/`findtime`/`bantime`, last ban time. Row click expands the banned-IP table with a per-row **Unban** button (confirm dialog) and a "Ban IP" input.
- **Settings tab**: global defaults (`bantime`, `findtime`, `maxretry`, `backend`), per-jail enable + overrides, and the `ignoreip` whitelist as a textarea of one CIDR/IP per line with client-side `netip` parity validation.
- Server selector reusing the monitor module's, since jails are per-server.
- Empty/absent state: when `fail2ban-client` is not installed the page renders an explanatory panel with the install hint — the same courtesy `showFail2ban()` attempted with its hardcoded howtoforge link, minus the dead link.
- `en.json` keys + an agent-browser E2E covering list → unban → list.

### D12 — Coexistence with UFW
fail2ban's `iptables-multiport`/`nftables-multiport` actions create their own `f2b-<jail>` chains, jumped to from `INPUT` ahead of UFW's chains. Nothing in `internal/firewall` enumerates or deletes foreign chains (`ufw.go` only issues `ufw allow`/`ufw delete allow` for its own diffed ports), so the two are independent — with one exception: `ufw --force reset` on `firewall_insert`/`firewall_delete` flushes everything, including fail2ban's chains. The fail2ban plugin therefore also subscribes to `firewall_insert`/`firewall_delete` and queues a `fail2ban` reload after them, so bans are re-installed. This is a one-line subscription, not a coupling: fail2ban re-reads its own database on reload.

## Risks / Trade-offs

- [Bans are live firewall mutations driven by HTTP] → admin-only + policy key, IP parsing before argv, jail name validated against the live jail list, self-ban refused (D6), every ban/unban logged with actor and source IP.
- [Config test failure leaves fail2ban stale] → write-temp/test/move ordering means the last known-good `jail.local` stays in place; the API surfaces the test output so the admin sees why the apply was rejected.
- [Missing logpath breaks the whole daemon, not just one jail] → D7's existence probe disables the offending jail instead of writing it; `backend = systemd` fallback covers rsyslog-less Debian 12+/Ubuntu 24.04.
- [Operator's hand-written `jail.local` is overwritten] → backed up to `jail.local~` on first managed write, with the managed-file header making ownership obvious; `jail.d/` drop-ins are never touched, giving an escape hatch that survives upgrades.
- [`ufw --force reset` flushes f2b chains] → D12 reload subscription; worst case is a short window where previously banned IPs are unblocked until the next offence.
- [The panel jail's failregex is new code, not a maintained upstream filter] → keep it narrow (one log line format the panel emits deliberately for failed logins), pin it with golden tests against real log samples, and default `maxretry` high enough that a fat-fingered admin is not banned.
- [`fail2ban-client` output format is not a stable API] → parsing lives in `client.go` behind one interface with golden-file tests over captured 0.11/1.0/1.1 outputs; a parse failure degrades to "unknown" in the UI rather than erroring the page.

## Migration Plan

- Code-only; no schema change. `[fail2ban]` is absent from existing `server.config` blobs and `getconf` decodes missing keys to zero values → the module treats "no section" as `enabled = n` and does nothing until an admin opts in.
- Fresh installs: `fail2banStep` seeds the defaults and writes the first `jail.local`, so new servers are protected out of the box.
- Existing installs (including ISPConfig3 migrations, where fail2ban was hand-configured per the tutorials): enabling the module backs up the operator's `jail.local` and takes ownership; `jail.d/` drop-ins keep applying. Documented in `docs/` alongside the firewall module notes.
- Rollback: disable the module in `config.toml`, restore `jail.local~`, `systemctl reload fail2ban`. Bans in fail2ban's own database are untouched throughout.

## Open Questions

- Should the `goisp-panel` jail's failregex live in this change, or should the panel first grow a dedicated structured auth-failure log line? Leaning: add the deliberate log line in this change (one `slog` call in the login handler) so the filter matches a format we control rather than scraping the general request log.
- Do we expose a "permanently allowlist this IP" button that appends to `ignoreip` from the banned-IP table, or keep whitelist editing confined to the Settings tab? Leaning: button, since it is the obvious next action after an unban.
- `recidive` is a Non-goal, but it is the single highest-value jail for repeat offenders. Revisit as a follow-up once the base jails have run in production.
- Whether the monitor `fail2ban` service probe should raise a warning state when the daemon is down on a server with jails enabled (monitor state semantics) or stay informational.
