# Proposal: refine-system-config-parity

## Why

Three gaps found by putting the shipped System module side by side with the PHP
panel running on the lab VM `192.168.56.20` (ISPConfig 3.3.1p1). All three are
places where an ISPConfig3 admin opens the equivalent screen here and finds
something missing.

**1. Server Config has no Server tab.** The legacy form renders nine tabs; we
render five. The Server tab was left out on the rule that a field with no
consumer in the Go daemon should not be rendered — and the rule holds for
`monit_*`/`munin_*`, which nothing reads. But it swept out fields that *are*
read (`[server] ip_address` in `internal/api/sitesdb.go`, `[server] ssh_port`
in `cmd/daemon.go`) and, more importantly, it left an adopted ISPConfig3
database with values an operator can see in the old panel and cannot see here.
Live on `.20` the tab has **26 fields**: network (ip/netmask/v6 prefix/gateway/
hostname/nameservers), firewall selector, log level and admin-notify level,
seven backup fields, monit and munin credentials, `log_retention` and
`migration_mode`.

**2. There is no System Config screen at all.** The legacy `System → Interface
Config` (heading "System Config", `interface/web/admin/system_config_edit.php`)
edits the panel-wide INI in `sys_ini`, and go-ispconfig **already reads it**:
`[misc] min_password_length` and `min_password_strength` gate every generated
password (`internal/api/sitesdb.go`), `[misc]`/`[sites] ssh_authentication`
gates shell-user auth (`internal/api/sites_ftp_shell.go`), `[sites]` carries the
database prefixes and phpMyAdmin URL, and `[mail]` drives welcome mail and the
rspamd level editor. Today the only way to change any of it is SQL. Live on
`.20` the screen has **6 tabs / 79 fields**: Sites 23, Mail 20, DNS 4,
Domains 2, Misc 28, DNS CAs 2.

**3. Remote Users grants look nothing like the legacy.** The PHP form renders
`remote_functions` as a **CHECKBOXARRAY of 58 function groups** ("Mail domain
functions", "Sites cron functions", …) covering **291 distinct function names**,
assembled at runtime from each module's `lib/remote.conf.php`. `add-api-tokens`
replaced that with 14 `resource:action` scopes — a deliberate choice
(design D4: our API is resource-oriented, so a new endpoint inherits its
group's scope instead of needing a grant name every existing token silently
lacks). The choice stands, but two consequences were never addressed: an
admin who knows ISPConfig3 does not recognise the screen, and a `remote_user`
row written by the PHP panel carries function names our parser reads as
unknown scopes, which denies everything.

## What Changes

- **Server Config gains the Server tab**, with the legacy field order and
  labels, split by a legend into what this port applies (`ip_address`,
  `ssh_port`) and what it stores for compatibility with an adopted database
  (the rest). Stored-only fields are labelled as such in the UI rather than
  silently pretending to work — the alternative, hiding them, is what produced
  this gap.
- **`ssh_port` becomes a first-class field** of that tab. It is a go-ispconfig
  extension with a real consumer (the firewall module's SSH allow rule) that
  has never been editable from the panel.
- **New System → Main Config screen** over `sys_ini`, reusing the
  section-per-tab editor built for Server Config, restricted to the keys the Go
  code reads, gated by `admin_allow_system_config`.
- **Remote Users renders the legacy function groups.** The 58 groups become the
  checkbox list the form shows, each mapping onto the scopes it implies;
  scopes remain the enforcement model underneath. An operator who ticks "Mail
  domain functions" gets `mail:write`, and the token still cannot exceed its
  owner.
- **Legacy `remote_functions` values are honoured.** A row written by the PHP
  panel (a bare CSV of function names) maps onto the equivalent scopes instead
  of parsing as unknown grants, so an adopted database's remote users keep
  working against the new API.
- The three docs gain the corresponding sections, and
  `docs/server-config-module.md`'s omission table is corrected where this
  change fills a gap it recorded.

## Capabilities

### New Capabilities

- `server-config-server-tab`: the `[server]` section editor — field set, the
  applied/stored split, `ssh_port`, and the validation of the fields that do
  have an effect.
- `interface-config`: the `sys_ini` editor — which tabs and keys are exposed,
  the security-policy gate, the password-policy validation, and the staleness
  guard that fails the build when code reads a key the form does not render.
- `remote-user-function-groups`: the legacy function-group presentation, its
  mapping onto scopes, and the compatibility parse of a PHP-written
  `remote_functions` value.

### Modified Capabilities

- `api-token-scopes`: scopes gain a documented mapping from the legacy function
  groups, and the parser gains the legacy-CSV compatibility path. The
  enforcement rules themselves do not change.

## Impact

- **Depends on** `server-config-sync` (the section-per-tab editor and its merge
  semantics), `add-api-tokens` (the scope model) and `cp-users`.
- **Supersedes** the `interface-config` capability sketched in
  `complete-system-module`: that proposal described it from the source, this one
  from the live panel with the tab and field counts confirmed. The remaining
  capabilities of `complete-system-module` (Server Services, Additional PHP
  Versions, Directive Snippets) are untouched and still pending.
- New `internal/api/systemconfig.go`, an extended
  `internal/api/serverconfigform.go`, a scope/function-group mapping table, and
  one Vue view; `internal/getconf` gains a decoded `[server]` section for the
  two keys that are read.
- **DB**: none. `sys_ini` and `server.config` both already exist and are
  already read.
- Operationally sensitive: Main Config edits the password policy every
  generated credential is checked against, so a bad value there is felt
  immediately across the panel.

## Non-goals

- **Making the stored-only Server fields do something.** Network configuration,
  backups and monit/munin belong to modules this port does not have; the tab
  stores and displays them, and says so.
- **Per-function grants as the enforcement model.** The 291 function names stay
  a *presentation* over scopes. Enforcing them individually would mean a grant
  table that every new endpoint has to be added to by hand — precisely the
  failure mode design D4 avoided.
- The legacy Misc tab's interface cosmetics (`custom_login_text`, dashboard
  atom feeds, `use_combobox`, `tab_change_warning`, maintenance mode) and the
  Branding tab: no consumer here, and unlike the Server tab they describe a PHP
  interface that does not exist in this panel.
- **DNS CAs** (`dns_ca` tab): the ACME CA registry belongs with the DNS module's
  certificate work, not with a config editor.
- The remaining `complete-system-module` surfaces — Server Services, Additional
  PHP Versions, Directive Snippets, `server_ip_map`.
