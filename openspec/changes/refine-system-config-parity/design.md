# Design: refine-system-config-parity

## Context

Three surfaces, one shared question: *what does an ISPConfig3 admin expect to
find here, and what does this port actually do with it?* The rule applied so
far — render only what the Go daemon consumes — is right for a knob that would
silently do nothing, and wrong when it hides a value the operator can see in
the old panel and change nowhere here.

What exists to build on:

- **The section-per-tab INI editor** from `server-config-sync`: a static
  `FormMeta`, `TabbedForm` on the SPA side, and a PUT that merges one section
  into the stored document so unrendered keys survive. `sys_ini` is the same
  shape as `server.config`, so Main Config is that editor pointed at a
  different row.
- **The scope model** from `add-api-tokens`: `resource:action`, declared per
  route group, intersected with the owner's own permissions.
- **`TestServerConfigFormMatchesGetconf`**, which asserts the rendered field
  set equals the keys `getconf` decodes. Adding a Server tab breaks it by
  construction — see D2.

Measured on the lab VM `192.168.56.20` (ISPConfig 3.3.1p1):

| Legacy screen | Shape |
|---|---|
| Server Config → Server | 26 fields |
| System Config | 6 tabs / 79 fields (Sites 23, Mail 20, DNS 4, Domains 2, Misc 28, DNS CAs 2) |
| Remote User → Functions | 58 checkbox groups over 291 function names, from 7 `remote.conf.php` files |

## Goals / Non-Goals

**Goals:**

- An adopted ISPConfig3 database shows the same `[server]` values here as in
  the old panel, and an operator can tell at a glance which ones this port acts
  on.
- Every `sys_ini` key the Go code reads is editable from the panel.
- A Remote Users screen an ISPConfig3 admin recognises, without abandoning the
  scope model underneath.
- A `remote_user` row written by the PHP panel keeps working.

**Non-Goals:**

- Implementing network configuration, backups or monit/munin.
- Per-function enforcement.
- Any schema change.

## Decisions

### D1 — The Server tab is split by a legend into "applied" and "stored"

**Decision**: render all 26 legacy fields plus `ssh_port`, in two groups:

```
Applied by this server
  ip_address        — database host suggestion (internal/api/sitesdb.go)
  ssh_port          — firewall SSH allow rule (cmd/daemon.go)

Stored for ISPConfig3 compatibility — not applied by this server
  auto_network_configuration, netmask, v6_prefix, gateway, hostname,
  nameservers, firewall, loglevel, admin_notify_events, backup_* (7),
  monit_* (3), munin_* (3), monitor_system_updates, log_retention,
  migration_mode
```

The second group renders under a **collapsible** legend, which `TabbedForm`
already supports (the DKIM block uses it).

**Why not hide them**: hiding is what created this gap. An operator migrating
from ISPConfig3 opens Server Config, sees no `backup_dir`, and cannot tell
whether the value was lost, ignored, or never read. Showing it under a legend
that says "stored, not applied" answers all three at once.

**Why not render them as disabled**: they are genuinely editable — the value
round-trips through the INI and the PHP panel would read it back. Disabling
would misrepresent that.

**Alternative considered**: a separate read-only "Legacy values" panel.
Rejected — same information, one more concept, and it breaks the tab-per-section
mapping that makes the save path one PUT per section.

### D2 — `getconf` gains a decoded `[server]` section, and the staleness test grows a second rule

**Decision**: add `getconf.ServerSection` with the two consumed keys
(`ip_address`, `ssh_port`), and extend `TestServerConfigFormMatchesGetconf` so
the `server` tab is checked against *two* sets: every decoded key must be
rendered (as today), and every additional rendered key must appear in an
explicit `serverCompatKeys` list in the source.

**Why the explicit list**: without it the test either fails on every
compatibility field or has to be turned off for that tab. A named list keeps
the guarantee that nothing appears in the form by accident, and reading it
tells you exactly which fields are compatibility-only — the list *is* the
documentation.

`internal/api/sitesdb.go` and `cmd/daemon.go` switch from `cfg.Raw["server"][…]`
to the decoded struct, so the two consumers stop reading the INI by hand.

### D3 — Main Config renders only what is read, and a grep-shaped test enforces it

**Decision**: `GET|PUT /api/system/config[/:section]` over `sys_ini` row 1,
same merge semantics as `/api/servers/:id/config`, gated by
`admin_allow_system_config`. Rendered keys:

| Tab | Keys |
|---|---|
| Sites | `dbname_prefix`, `dbuser_prefix`, `ftpuser_prefix`, `shelluser_prefix`, `phpmyadmin_url`, `default_dbserver`, `default_remote_dbserver`, `disable_client_remote_dbserver`, `ssh_authentication` |
| Mail | the welcome-mail keys and `rspamd_spam_tag_level` / `rspamd_spam_kill_level` / `rspamd_greylisting_level` |
| Misc | `min_password_length`, `min_password_strength`, `ssh_authentication` |

**The staleness guard** scans the codebase for `sections["<tab>"]["<key>"]`
literals and fails when one has no rendered field. It is grep-shaped and will
need updating if the access pattern changes — which is cheaper than a form that
silently omits the key someone's password policy depends on. This is the same
bug class that left `[auth] jwt_secret` out of `config.toml.example`, caught by
the same kind of test.

**Why not the other 60 legacy fields**: they configure a PHP interface that
does not exist here — dashboard atom feeds, `use_combobox`, custom login text.
Unlike the Server tab's compatibility fields, they do not describe the *server*,
so an adopted database has nothing meaningful to show for them.

### D4 — Function groups are a presentation over scopes, not a second grant model

**Decision**: a static table maps each of the 58 legacy function groups onto the
scopes it implies:

```
"Mail domain functions"        → mail:write
"Sites cron functions"         → sites:write
"Server functions"             → server:read
"Record permission changes"    → system:write
…
```

The Remote Users form renders the groups (the labels an ISPConfig3 admin
knows); the token stores scopes. Ticking several groups unions their scopes.
The form also shows the resulting scope list, so the mapping is never a black
box — an operator can see that three groups collapsed into `mail:write`.

**Why not enforce per function**: 291 grant names that every new endpoint must
be added to by hand. The first endpoint someone forgets is an endpoint every
existing token can suddenly reach, or one none of them can — both silent. Route
groups carry scopes today precisely so that cannot happen (design D4 of
`add-api-tokens`).

**Why not drop the groups and show raw scopes**: we already do, and the user
comparing screens with `.20` did not recognise it. Fourteen strings are easier
to reason about and harder to *find* — "which of these lets my billing system
create mailboxes?" is answered by a label, not by a grammar.

**Asymmetry, stated plainly**: the mapping is many-to-one and therefore lossy in
one direction. Ticking "Mail domain functions" and "Mail user functions" both
yield `mail:write`, so unticking one afterwards cannot remove the other's
grant. The form handles this by treating the scope list as the source of truth
and re-deriving which groups are checked from it — a group shows as checked
when its scopes are a subset of the token's. Two groups that map to the same
scope therefore check and uncheck together, which is honest: they *are* the
same grant here.

### D5 — Legacy `remote_functions` maps forward, never sideways

**Decision**: `apitoken.ParseMeta` already accepts a bare CSV. Extend it so a
CSV whose entries look like legacy function names (`^[a-z]+_[a-z_]+$`, no colon)
is translated through the group table into scopes, instead of being kept as
unknown grants that match nothing.

**Why translate rather than deny**: an adopted database's remote users are
credentials someone is actively using against the PHP panel's API. Denying them
turns a migration into an outage; translating them keeps the same *shape* of
access. The translation is deliberately one-way — we never write legacy names
back — so the row converges to the new format on first save.

**Risk accepted**: an unrecognised function name maps to nothing. If *no* name
in the CSV maps, the token grants nothing and is denied, which is the safe
direction.

## Risks / Trade-offs

- **Compatibility fields look editable but do nothing** → the collapsible
  legend states it, `docs/server-config-module.md` lists them, and they are
  named in `serverCompatKeys` in the source. Anyone who reads any of the three
  learns the same fact.
- **Main Config can set a password policy the panel cannot satisfy** → the
  policy is validated on save (numeric, within the accepted maximum) and a
  refusal names the field.
- **The function-group mapping is lossy** → D4's re-derivation makes the UI
  consistent with what is actually stored, and the resulting scope list is
  displayed rather than hidden.
- **The grep-shaped `sys_ini` test is brittle** → it fails loudly rather than
  silently, and the failure names the key. A false positive costs one line in
  the form; a false negative costs an operator their password policy.
- **`ssh_port` becomes editable** → a wrong value locks the operator out of SSH
  through the firewall rule. Validate the range on save, and keep the
  daemon-side fallback to port 22 that already exists when the value does not
  parse.

## Migration Plan

1. Server tab first: it is additive, reuses the landed editor, and its two
   consumed keys can be verified end to end (change `ip_address`, create a
   client database, see the host suggestion follow).
2. `getconf.ServerSection` and the switch of the two existing consumers, so the
   staleness test covers the tab from the start.
3. Main Config, validated by raising `min_password_length` and watching a
   shorter database-user password get refused.
4. Function groups last — the mapping table is mechanical, but it is the piece
   whose UI needs a human to look at it next to `.20`.
5. **Rollback**: each surface is additive; removing a route removes the screen
   and leaves the data untouched. The `remote_functions` translation is
   read-only until a row is saved.

## Open Questions

- Should the compatibility fields be writable at all, or read-only until a
  module claims them? Writable is proposed, because the PHP panel on the same
  database would write them anyway.
- `log_retention` has an obvious analogue in `[daemon] datalog_retention_days`.
  Should the Server tab's value drive it, or stay compatibility-only? Proposed:
  compatibility-only for now, because one is a per-server INI key and the other
  is process config, and silently binding them would surprise both ways.
- Does the function-group list belong in the API (served like
  `/api/tokens/scopes`) or in the frontend? Serving it keeps one source of
  truth for the mapping the parser also uses.
