# Tasks: ui-mail-login-aaa

## 1. Login layout
- [x] 1.1 LoginView: brand/card ~15% from top (not vertically centered). Commit.
- [x] 1.2 Screenshot `docs/prints/login-top-15.png` + redeploy smoke. Commit script/docs only if needed.

## 2. Mailbox form AAA
- [x] 2.1 Add `MailboxForm.vue` with name, local-part + domain select, compose email. Commit.
- [x] 2.2 Password generate + strength + repeat; wire create/update. Commit.
- [x] 2.3 Quota MB UI ↔ bytes API; optional CC/BCC/IMAP helpers. Commit.
- [x] 2.4 Spamfilter policy select if supported; hide derived system fields. Commit.
- [x] 2.5 Router + i18n; drop EntityForm-only path for mailboxes create/edit. Commit.
- [x] 2.6 E2E mail create/edit against domain select; screenshots `docs/prints/mailbox-form-*-aaa.png`. Commit.

- [x] 2.7 Tabs parity: Mailbox | Autoresponder | Mail Filter | Custom Rules | Backup (+ backup fields in API metadata). Commit.

## 3. Review / ship
- [x] 3.1 Claude Fable 5 write-up `.hermes/review-fable-mailbox-aaa.md`.
- [x] 3.2 Grok final review + residual fixes (tabs + admin Custom Rules + forward_in_lda).
- [x] 3.3 Redeploy lab `.10`; Telegram MEDIA; PR to main; archive change when green.
