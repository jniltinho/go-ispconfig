# Cron module

In-process client job scheduler for go-ispconfig (OpenSpec `add-cron-module`).

## Architecture

- **API** (`/api/sites/crons`): declarative entity with schedule/command validation,
  type auto-derivation (`url` / `chrooted` / `full`), client limits
  (`limit_cron`, `limit_cron_type`, `limit_cron_frequency`), and
  `GET /api/sites/crons/:id/runs` from `sys_log` (`cron_run id=…`).
- **Module** (`internal/cron`): table hook on `cron` → `cron_insert|update|delete`,
  gated on `server.web_server=1` and config enablement.
- **Plugin + ClientJobRunner**: keeps an in-process `robfig/cron` registry; never
  writes under legacy `crontab_dir` for new jobs. On load, removes leftover
  `ispc_*` / `ispc_chrooted_*` / `*.cron` files (legacy cutover).
- **Executors**:
  - **URL**: HTTP GET, `{DOMAIN}` expansion, TLS verify on.
  - **full / chrooted**: argv without shell, placeholders, cwd under site docroot,
    **privilege drop** to site uid/gid (refuse root).

## Schedule fields

Five classic cron fields (`run_min` … `run_wday`) plus `run_month=@reboot`
(run-once-on-daemon-start, not on every save). Frequency helpers enforce
client `limit_cron_frequency`.

## UI

Sites → **Cron**: list, form (parent vhost, schedule, command, log/active),
run history on edit.

## Lab / E2E

```bash
make e2e-cron PANEL_URL=https://127.0.0.1:8096 ADMIN_PASSWORD=...
```

Redeploy lab `.10` after merge (`make build-linux` + vagrant upload).

## Migration notes

Import from ISPConfig PHP keeps the same `cron` table shape. Daemon cutover
deletes PHP-style crontab drop-ins; jobs are owned by the Go runner thereafter.
