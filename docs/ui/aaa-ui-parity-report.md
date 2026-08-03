# AAA UI parity pass — go-ispconfig vs legacy ISPConfig3

Visual comparison of the Go panel (lab VM `192.168.56.10:8080`) against the
legacy PHP panel, at 1440×900. Reference prints live next to this file and in
`.hermes/prints/`.

## Closed in this pass

| Area | Legacy | Was | Now |
|---|---|---|---|
| Topbar tabs | 9 modules | 7 (no Help/Tools) | 9, legacy order |
| Dashboard title | `Welcome admin` | `Dashboard` | `Welcome <user>` |
| Dashboard cards | fixed 4 columns, `Available Modules` heading | auto-fill grid, no heading | 4 columns + heading |
| Dashlet head | icon left, title beside it | title flush right | title centred beside the icon |
| Login | centred brand, placeholder fields, buttons side by side | tinted head, stacked labels, split actions | matches |
| List filter row | light band under the dark head | on the dark head | light band |
| Sidebar | 2nd-level section bars per group | one module-name bar | group bars (sites, mail, client, monitor, system) |
| System → Server IPs | fits the 1260px wrapper | reported overflow | fits (no overflow at 1440) |

## Remaining deltas (deliberate or out of scope)

1. **Brand mark** — legacy uses the full red `ISPCONFIG` wordmark; we use the
   go-ispconfig icon plus a text logotype. Branding decision, not a bug.
2. **`Dashboard` vs `Home`** — legacy labels the first tab `Home`. Kept
   `Dashboard` because the sidebar and page title reuse the same key.
3. **`Latest news` sidebar panel** — legacy shows an RSS news list on the
   dashboard. No feed exists on our side; would need a backend endpoint.
4. **Button chrome** — legacy buttons are rounded gradients (Bootstrap 3);
   ours are flat squares from the current token set. Deliberate.
5. **Row delete action** — legacy is a solid red button, ours a light outline.
6. **Extra dashlets** — server state / pending jobs / failed jobs and the
   System metrics charts have no legacy dashboard equivalent (legacy keeps
   the charts under Monitor). Kept as an improvement.
