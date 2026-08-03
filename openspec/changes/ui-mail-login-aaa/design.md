# Design: ui-mail-login-aaa

## Approach
1. Keep API `email` as full address and `quota` as bytes — UI composes/converts.
2. Prefer dedicated `MailboxForm.vue` over stretching generic EntityForm for compound controls.
3. Domain options from existing mail domains list API (primary domains only).
4. Login: CSS-only layout change (`pt-[15vh]` / no `items-center` vertical center).
5. Fable 5 implements → Grok reviews diff against PHP tform + screenshots.

## Risks
- Edit mode domain immutability: match PHP (often lock domain after create).
- Quota unit mistakes (KiB vs MB) — document MB = 1024² bytes.
- E2E may assume EntityForm selectors — update selectors.
