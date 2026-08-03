# Proposal: ui-mail-login-aaa

## Why

The first cross-module UI QA pass improved filters and generic DataTable/EntityForm consistency, but **did not reach AAA parity** with ISPConfig3 for high-traffic mail UX and the login entry point.

Concrete regressions vs legacy lab `192.168.56.20` (admin screenshots):

1. **Mailbox create/edit** (`/mail/mailboxes/new`) is a flat EntityForm dump: single free-text email, password without generate/strength/repeat, quota in raw bytes, missing domain select, weak optional hints, no spamfilter policy control, poor field order vs `mail_user.tform.php`.
2. **Login** vertically centers the card; product expectation is brand/logo ~**15% from the top** of the viewport (not middle of the screen).

This change is a **focused AAA polish** for mail mailbox forms + login layout, validated against PHP tform + live legacy panel.

## What Changes

- Dedicated **MailboxForm** Vue view (not generic EntityForm-only) wired in the router for create/edit.
- Email UX: local-part + `@` + **domain SELECT** (primary `mail_domain` only); compose/split for API `email`.
- Password UX: generate button, strength indicator, repeat-password client check.
- Quota UX: edit/display in **MB** (0 = unlimited); convert to/from API **bytes**.
- Optional fields/helps aligned with PHP (CC, BCC, IMAP prefix, copy-during-delivery when modeled).
- Spamfilter policy select when API/model supports it.
- Hide/move derived system fields (maildir, uid, gid, …) off the main tab or admin Options.
- LoginView: stop vertical centering; place card/logo ~`15vh` from top.
- i18n keys, e2e updates (`e2e/panel-mail.sh` / login smoke), lab redeploy `.10`, Telegram MEDIA screenshots.
- Agent split: **Claude Fable 5** implements UI; **Grok** final review/fixes; Hermes orchestrates kanban/status.

## Capabilities

### New
- `mail-mailbox-form-aaa`: dedicated mailbox form with domain select and legacy-aligned controls.
- `login-layout-top`: login brand block positioned ~15% from viewport top.

### Modified
- `mail-panel-ui` / mailbox entity metadata usage (frontend primarily; small API lookups only if required).
- Login route view.

## Impact

- Frontend: `LoginView.vue`, new `MailboxForm.vue`, router, `en.json`, mail e2e.
- Backend: only if lookup endpoints or metadata need extension (prefer no schema change).
- PHP reference: `interface/web/mail/form/mail_user.tform.php`, `mail_user_edit.php`.
- Labs: validate on `.10`; compare UX to `.20` (read-only).

## Non-goals

- Full mail module rewrite (aliases/forwards/catchalls deep redesign).
- New mail backend features (SIEVE editor, full autoresponder overhaul) unless already half-wired.
- Global redesign of every form (out of scope — this is mail+login AAA).
- PowerDNS / monitor module work (separate changes).

## Agents / process

| Step | Who |
|---|---|
| Implement | Claude **Fable 5** (`feat/ui-aaa-mailbox-login`) |
| Final review + residual fixes | **Grok** |
| Orchestration, OpenSpec, kanban, Telegram | Hermes |

## Acceptance

- [ ] Login: logo/card starts ~15% from top (screenshot).
- [ ] Mailbox new: domain select visible; local@domain save works on lab `.10`.
- [ ] Generate password + repeat; quota MB round-trip.
- [ ] `npm run build` green; mail e2e updated/green as applicable.
- [ ] Grok review notes filed; PR ready.
