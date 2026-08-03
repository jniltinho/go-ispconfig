# Tasks: ui-mail-login-aaa

## 1. Login layout
- [ ] 1.1 LoginView: brand/card ~15% from top (not vertically centered). Commit.
- [ ] 1.2 Screenshot `docs/prints/login-top-15.png` + redeploy smoke. Commit script/docs only if needed.

## 2. Mailbox form AAA
- [ ] 2.1 Add `MailboxForm.vue` with name, local-part + domain select, compose email. Commit.
- [ ] 2.2 Password generate + strength + repeat; wire create/update. Commit.
- [ ] 2.3 Quota MB UI ↔ bytes API; optional CC/BCC/IMAP helpers. Commit.
- [ ] 2.4 Spamfilter policy select if supported; hide derived system fields. Commit.
- [ ] 2.5 Router + i18n; drop EntityForm-only path for mailboxes create/edit. Commit.
- [ ] 2.6 E2E mail create/edit against domain select; screenshots `docs/prints/mailbox-form-*-aaa.png`. Commit.

## 3. Review / ship
- [ ] 3.1 Claude Fable 5 write-up `.hermes/review-fable-mailbox-aaa.md`.
- [ ] 3.2 Grok final review + residual fixes.
- [ ] 3.3 Redeploy lab `.10`; Telegram MEDIA; PR to main; archive change when green.
