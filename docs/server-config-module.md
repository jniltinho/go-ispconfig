# Server Config (System → Server Config)

Per-server configuration editor: the `server.config` INI blob of one node,
edited one section per tab. Port of the legacy
`interface/web/admin/server_config_edit.php`, validated field by field against
the PHP panel on the lab VM `192.168.56.20` (ISPConfig 3.3.1p1).

A save merges the submitted keys into the section, re-serialises the whole INI
and journals a `sys_datalog` row for `dbtable=server`, so the owning node picks
the change up on its next cycle. Keys the panel does not render survive the
round trip — the merge never rewrites the rest of the document.

- UI: `/system/server-config` (server list) → `/system/server-config/:id`
- API: `GET /api/meta/forms/server_config`, `GET|PUT /api/servers/:id/config[/:section]`
- Admin only, on every route.

## Tabs

One tab per INI section, and one section per `internal/getconf` config struct:

| Tab | INI section | Fields | Consumed by |
|---|---|---:|---|
| Server | `[server]` | 2 + 25 | `internal/api/sitesdb.go` (database host suggestion), `cmd/daemon.go` (firewall SSH port) — plus 25 compatibility fields, see below |
| Web | `[web]` | 74 | `internal/web`, `internal/jailkit` (vhosts, PHP-FPM, Let's Encrypt, quota notices) |
| DNS | `[dns]` | 12 | `internal/dns`, `internal/powerdns` (zonefiles, named.conf, DNSSEC, backend switch) |
| Mail | `[mail]` | 30 | `internal/mail` (Postfix/Dovecot/Rspamd paths, DKIM, relayhost, quotas) |
| Getmail | `[getmail]` | 3 | `internal/mail` fetchmail rendering |
| Jailkit | `[jailkit]` | 6 | `internal/jailkit` (chroot home, app sections/programs) |

The field table is generated from the legacy tform plus
`en_server_config.lng` and lives in `internal/api/serverconfigform.go`.
`TestServerConfigFormMatchesGetconf` fails the build if a `getconf` struct
gains or loses an `ini` tag without the form following — the editor can never
drift out of sync with what the daemon reads.

## The Server tab: applied vs. stored

Two `[server]` keys are acted on by this port and appear at the top of the tab:

| Key | Read by |
|---|---|
| `ip_address` | the host suggestion when a client database is created (`internal/api/sitesdb.go`) |
| `ssh_port` | the firewall module's SSH allow rule (`cmd/daemon.go`). A go-ispconfig extension — ISPConfig3 has no such field. Validated as 1–65535 on save; an unparseable stored value leaves the daemon's fallback to port 22 in place |

Below them, behind a collapsed legend reading **"Stored for ISPConfig3
compatibility — not applied by this server"**, sit the remaining 25 legacy
fields: network configuration, the firewall selector, log level and
admin-notify level, the seven backup fields, monit and munin credentials,
`monitor_system_updates`, `log_retention` and `migration_mode`.

They round trip through the INI and are shown to the operator, but **nothing in
this port reads them**. They are rendered anyway because hiding them is what
made an adopted ISPConfig3 database look like it had lost values the operator
could still see in the old panel. The list lives in `serverCompatKeys`
(`internal/api/serverconfigform.go`), and `TestServerCompatKeysAreNotDecoded`
fails if one of them ever gains a consumer without being moved above the
legend.

## Deliberate omissions

The legacy form renders 9 tabs. The three that are still absent here have **no
consumer in the Go daemon**: rendering them would let an operator set a value
that silently does nothing.

| Legacy tab | Why it is not rendered |
|---|---|
| Cron | The scheduler is internal to the daemon; there is no crontab writer to point at `init_script`/`crontab_dir`/`wget` (see [cron-module.md](cron-module.md)). |
| Rescue | `try_rescue` and friends drive the PHP monitor's service-restart loop, which has no port. |
| XMPP / FastCGI / vlogger / UFW | XMPP is a future module (see [ROADMAP.md](ROADMAP.md)); FastCGI and vlogger are Apache-era config the nginx/PHP-FPM path never reads; UFW rules are managed by the Firewall module, not by INI keys. |

Per-field omissions inside a rendered tab, same rule:

- **Web**: `website_autoalias`, `nginx_enable_pagespeed`, `CA_path`, `CA_pass`,
  `php_handler`, `php_fpm_incron_reload`.
- **Mail**: `rspamd_available`, `reject_sender_login_mismatch`,
  `reject_unknown`, `realtime_blackhole_list`, `stress_adaptive`,
  `mailbox_quota_stats`, and the `overquota_notify_*` family (the web section
  carries the quota-notification keys the daemon actually reads).
  `rspamd_redis_password` / `rspamd_redis_bayes_password` are spelled
  `rspamd_redis_passwd` / `rspamd_redis_bayes_passwd` here, which is what
  `getconf` decodes.

Fields with **no legacy counterpart** (go-ispconfig extensions) are rendered
too: `dns_backend`, `powerdns_axfr_conf`, `dnssec_resign_days`,
`vhost_proxy_protocol_protocols`, `sendmail_path`, `getmail_program`,
`getmail_user`, and the `rspamd_spam_*` / `rspamd_greylisting_level` levels.

## Save semantics

The editor PUTs **only the sections whose values changed**, comparing against
what the form loaded with rather than against the raw INI. A key absent from
the stored INI shows its field default — which `getconf` already applies — so
touching one tab does not materialise defaults into the other four, and one
save produces exactly one `sys_datalog` row.

The API refuses a section that would be stored empty, and refuses any key or
value the INI grammar cannot express (`^[\w\d_]+$` keys, no CR/LF in values):
either would silently drop stored keys on the next parse.

## System module parity with the legacy panel

Checked against `admin/lib/module.conf.php` on `192.168.56.20`:

| Legacy entry | go-ispconfig |
|---|---|
| Server Config | **this module** |
| Server IP addresses | `/system/server-ips` |
| CP Users | sidebar entry exists, still the module placeholder |
| Firewall | `/system/firewall` (UFW rule sets) |
| Remote Users | not implemented — there is no remote JSON API to grant against |
| Server Services | not implemented — the `server` role flags are editable through `/api/server` but have no form |
| Server IPv4 mapping | not implemented (`server_ip_map`) |
| Additional PHP Versions | not implemented (`server_php`) |
| Directive Snippets | not implemented (`directive_snippets`) |
| Firewall IPTables / Packet Filter / Port Forward | not implemented — the firewall module is UFW-only |
| Interface Config | `/system/main-config` — the `sys_ini` keys this port reads (see below) |
| Extension Installer, Remote Actions | not planned — no ISPConfig extension repo or PHP updater to drive |

go-ispconfig adds two System entries the legacy panel has no equivalent for:
**Fail2ban** (`/system/fail2ban`) and **Migrate from ISPConfig3**
(`/system/migration`, see [legacy-migration.md](legacy-migration.md)).

## Main Config (System → Main Config)

The panel-wide INI in `sys_ini`, port of
`interface/web/admin/system_config_edit.php`. Same section-per-tab editor and
the same merge semantics as Server Config, pointed at a different row.

| Tab | Keys | Read by |
|---|---|---|
| Sites | `dbname_prefix`, `dbuser_prefix`, `ftpuser_prefix`, `shelluser_prefix`, `phpmyadmin_url`, `default_dbserver`, `default_remote_dbserver`, `disable_client_remote_dbserver`, `ssh_authentication` | `internal/api/sitesdb.go`, `internal/api/sites_ftp_shell.go` |
| Mail | `enable_welcome_mail`, `admin_mail`, `admin_name`, `show_per_domain_relay_options`, `rspamd_spam_tag_level`, `rspamd_spam_kill_level`, `rspamd_greylisting_level` | `internal/mail/welcome.go`, `internal/api/mail.go`, `internal/api/mailrspamd.go` |
| Misc | `min_password_length`, `min_password_strength` | `internal/api/sitesdb.go` |

The legacy screen renders 6 tabs and 79 fields. The rest configure a PHP
interface that does not exist here — dashboard atom feeds, `use_combobox`,
custom login text, maintenance mode — so an adopted database has nothing
meaningful to show for them. Unlike the Server tab's compatibility fields,
they do not describe the *server*, which is why they are omitted rather than
shown behind a legend.

`TestSysIniFormCoversEveryRead` is the guard: it scans the tree for `sys_ini`
reads and fails when a key has no field on this form. It is a grep, and a new
access shape could slip past it — that ceiling is stated in the test — but it
already caught two keys (`ftpuser_prefix`, `shelluser_prefix`) that a manual
sweep of the same code had missed.

Two deliberate semantics, both matching the legacy panel:

- **any key the INI grammar accepts may be written**, not just the rendered
  ones. That is what lets an adopted ISPConfig3 database keep working; the
  surface is admin-only behind `admin_allow_system_config`;
- **a merge never removes a key.** Clearing a field stores an empty value and
  the key stays in the blob.

### Defaults come from the installer template, not the tform

`system_config.tform.php` ships an empty default for every prefix and
`min_password_length=5`. Those are **not** the values a real ISPConfig install
carries — its installer writes `install/tpl/system.ini.master` over the empty
`sys_ini` row (`installer_base.lib.php:431`), and that template says:

| Key | Value |
|---|---|
| `dbname_prefix`, `dbuser_prefix` | `c[CLIENTID]` |
| `ftpuser_prefix`, `shelluser_prefix` | `[CLIENTNAME]` |
| `phpmyadmin_url` | `https://[SERVERNAME]:8081/phpmyadmin` |
| `ssh_authentication` | *(empty)* |
| `enable_welcome_mail` | `y` |
| `show_per_domain_relay_options` | `n` |
| `min_password_length` | `8` |
| `min_password_strength` | `3` |

Verified byte for byte against the live `sys_ini` of the lab panel at
`192.168.56.20`. `internal/database/system_config.ini` seeds the same values,
and `Seed()` writes it **only when the row is still empty**, so an adopted
ISPConfig3 database keeps its own. Without that seed a fresh install had an
empty `sys_ini`, and the divergence showed up in the data rather than in the
UI: `dbname_prefix` is what makes a client database `c1_shop` instead of
`shop`.

`TestSysIniDefaultsMatchLegacyTemplate` pins the form defaults and
`TestSeededSysIniMatchesFormDefaults` keeps the seed and the form from
drifting apart — a form showing `c[CLIENTID]` over a database seeded with
nothing would be a lie that only surfaces at the first client database.

**`httpuser` and `httpgroup` do not exist in ISPConfig 3.3.1p1** (no hit
anywhere in the 3.3.1p1 tree). The equivalent keys are `[web] user` and
`[web] group` in `server.config`, defaulting to `www-data`, and they live on
the Server Config Web tab. `mailuser_uid`, `mailuser_gid` and `mailuser_name`
are likewise `server.config` `[mail]` keys, not `sys_ini` — already on the
Server Config Mail tab with the legacy defaults `5000`, `5000` and `vmail`.

The password policy is validated on save: a minimum length outside 1–64, or a
strength outside 1–3, is refused with a field error rather than stored — the
panel would otherwise hold a policy its own generated credentials could not
satisfy. It takes effect immediately: raise the minimum and the next database
user with a shorter password is refused by the API.
