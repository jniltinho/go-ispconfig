# Proposal: add-fail2ban-module

> Greenfield module — there is **no** PHP plugin to port. See "No PHP counterpart" below before reading the rest.

## Why

Every go-ispconfig install exposes SSH, SMTP/submission, IMAP/POP3, FTP and the panel login itself to the public internet. The firewall module (`add-firewall-module`) opens those ports and explicitly declared fail2ban a non-goal ("fail2ban installation or jail management (monitoring shows its status only)"), and the monitor module deferred the `log_fail2ban` collector as a stub. The result today is that nothing in the stack rate-limits or bans credential-stuffing against the services the panel provisions, and an admin who installs fail2ban by hand gets zero visibility from the panel.

### No PHP counterpart

This is a **new capability with no direct PHP plugin to port**. Confirmed by reading `base/ispconfig3_install/`:

- **There is no `server/plugins-available/fail2ban_plugin.inc.php`.** The directory contains 37 plugins (`firewall_plugin.inc.php`, `iptables_plugin.inc.php`, `apache2_plugin.inc.php`, …) and none of them is fail2ban. There is likewise no `fail2ban` module under `server/mods-available/`.
- `install/lib/installer_base.lib.php:2701` defines `configure_fail2ban()` whose entire body is `// To Do`. The Debian override `install/dist/lib/debian60.lib.php:238` has a body too, but every line in it is **commented out**:
  ```php
  public function configure_fail2ban() {
      /*
      copy('tpl/dovecot-pop3imap.conf.master',"/etc/fail2ban/filter.d/dovecot-pop3imap.conf");
      copy('tpl/dovecot_fail2ban_jail.local.master','/etc/fail2ban/jail.local');
      */
  }
  ```
  `install/install.php:566-570` still prompts "Configuring Fail2ban" and calls the stub, so the installer *claims* to configure fail2ban and does nothing.
- The only surviving artifacts are two orphaned templates never copied by any live code path: `install/tpl/dovecot_fail2ban_jail.local.master` (a single `[dovecot-pop3imap]` jail) and `install/tpl/dovecot-pop3imap.conf.master` (its filter). **`server/conf/` contains no fail2ban master at all.**
- `install/lib/installer_base.lib.php:281` only detects presence (`is_installed('fail2ban-server')` → `$conf['fail2ban']['installed']`), and `helper_scripts/gentoo_setup.sh:758` emerges the package on Gentoo. Neither writes a jail.
- **ISPConfig3 has no fail2ban UI.** The panel's entire fail2ban surface is a raw log dump: `server/lib/classes/cron.d/100-monitor_fail2ban.inc.php` tails `/var/log/fail2ban.log` (path from `server/lib/classes/monitor_tools.inc.php:663-673`) into `monitor_data` as type `log_fail2ban` with `state = 'no_state'`, and `interface/lib/classes/tools_monitor.inc.php:474 showFail2ban()` renders it with `nl2br()` behind `monitor/show_data.php?type=fail2ban`. No jail list, no banned-IP list, no unban action, no whitelist editing — an admin must SSH in and run `fail2ban-client`.

So the fail2ban jails on every ISPConfig3 "Perfect Server" install come from the *tutorial*, hand-pasted by the operator, not from ISPConfig code. This change makes them a first-class, panel-managed module modeled on the firewall module (`internal/firewall`), which is the closest analogue: one server-scoped config surface, an admin-only entity, a daemon plugin that shells out through the foundation `CommandRunner`, and a lock-out guard.

## What Changes

- **fail2ban module (daemon side)**: Go `Module` in `internal/fail2ban` modeled on `internal/firewall/module.go` — registers the table hook for the new server-scoped config surface and announces `fail2ban_insert/update/delete`; registers the `fail2ban` service for delayed reload through the foundation services registry.
- **fail2ban plugin**: renders `/etc/fail2ban/jail.local` (Debian/Ubuntu override file, not `jail.conf`) plus go-ispconfig-owned filter files under `/etc/fail2ban/filter.d/`, from a jail catalogue gated by which services the local `server` row actually runs (web/mail/dns/firewall flags + binary probes). Applies via `fail2ban-client reload` with a config test first; never restarts blindly.
- **Jail catalogue**: `sshd`, `postfix`, `postfix-sasl`, `dovecot`, `pure-ftpd` (matching the `pure-ftpd-mysql` package the installer already provisions in `internal/installer/ftpstep.go`) and a new `goisp-panel` jail + filter for the panel's own failed-login log lines. Per-jail `enabled`, `maxretry`, `findtime`, `bantime`, `logpath`/`backend`, `action`, plus a global `ignoreip` whitelist.
- **Live-state REST API**: `/api/fail2ban/jails` (list with currently-banned counts), `/api/fail2ban/jails/{jail}` (status detail incl. banned IP list), `POST /api/fail2ban/jails/{jail}/unban` and `POST /api/fail2ban/jails/{jail}/ban`, driven by `fail2ban-client status <jail>` / `set <jail> unbanip <ip>` / `set <jail> banip <ip>`. Admin-only, swaggo-annotated, same policy pattern as `internal/api/firewall.go`.
- **Config CRUD**: admin-only `fail2ban` entity (`internal/api/fail2ban.go`) over the jail settings, using the existing declarative `Entity` framework and datalog writes, exactly like `firewallEntity()`.
- **UI (Vue 3)**: `System → Fail2ban` page next to `System → Firewall` — jail list with ban counts, per-jail banned-IP table with an Unban button, whitelist (`ignoreip`) editor and per-jail tuning form.
- **Installer step**: `fail2ban` package install + `systemctl enable --now fail2ban` in `internal/installer` (new `fail2banStep`, same shape as `ftpStep`), so a fresh install is protected by default instead of the ISPConfig3 no-op.
- **Monitor**: fill in the `log_fail2ban` collector the monitor module deferred, and add a `fail2ban` service probe so the existing monitor state page reports the daemon.

## Capabilities

### New Capabilities

- `fail2ban-jails`: daemon module + plugin — jail.local/filter.d rendering from the config surface, service gating, config test and reload, installer provisioning.
- `fail2ban-rules`: the jail catalogue itself — which services are watched, per-distro log paths and backends, default `maxretry`/`findtime`/`bantime`/`action`, `ignoreip` whitelist semantics and the panel filter regex.
- `fail2ban-unban-ui`: live-state REST API (`fail2ban-client status` / `unbanip` / `banip`) and the Vue admin page — jail list, banned IPs, unban/ban actions, whitelist editing.

### Modified Capabilities

- `firewall-panel-ui`: the System section gains a sibling `Fail2ban` entry; the firewall page cross-links to it. No change to firewall behaviour.

## Impact

- **Depends on `port-ispconfig3-to-go`** (datalog registries, `.master` renderer, getconf, delayed service restarts, `CommandRunner`, rest-api-core, auth-permissions, panel-skeleton) and reuses the patterns established by `add-firewall-module` and `add-monitor-module`.
- New Go package `internal/fail2ban` (module + plugin + client wrapper), `internal/api/fail2ban.go`, `internal/installer/fail2banstep.go`, Vue `System → Fail2ban` view.
- **DB**: ISPConfig3 has no fail2ban table. Jail settings live in the existing `server.config` INI blob under a new `[fail2ban]` section read through `internal/getconf` — **no schema change, no migration**. See design D2.
- External: `fail2ban` package (Debian 11–13, Ubuntu 22.04/24.04 — the five distros `internal/installer/distro.go` supports), `fail2ban-client` on PATH, and the `iptables`/`nftables` backend fail2ban chooses. Interacts with UFW: fail2ban inserts its own chains, so the firewall module's rules and fail2ban's bans coexist without either owning the other's chains.
- Security-sensitive: unban/ban endpoints mutate live firewall state from HTTP. Admin-only, IP-validated, rate-limited, and every action logged (design D6).

## Non-goals

- Porting `iptables_plugin.inc.php` or replacing the UFW path in `internal/firewall` — fail2ban manages its own chains.
- Per-client or per-website jails, and exposing ban/unban to non-admin panel users.
- fail2ban's mail notification actions, `recidive` cross-jail escalation, `blocklist.de`/AbuseIPDB reporting actions, and Cloudflare/API-based actions.
- Multi-server aggregation: jails and bans are per-server; the UI shows one server at a time (server selector), no cluster-wide ban list.
- Replacing fail2ban with an in-Go log scanner, or supporting CrowdSec.
- Apache/nginx HTTP-auth jails (`nginx-http-auth`, `apache-auth`) — the panel jail covers the login surface we own; web jails can follow once `web_folder_user` auth logs are stable.
- Distros outside the installer's supported set (no Rocky/Alma/openSUSE/Gentoo paths).
- Translations beyond English.
