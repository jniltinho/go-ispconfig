# Design: ui-forms-tables-qa

## Approach
1. Inventory first (no big rewrite).
2. Module-by-module agent-browser + checklist.
3. Grok implements code fixes on branch `feat/ui-forms-tables-qa`.
4. Claude Fable 5 reviews UI quality after each module batch and at final polish (4.3).
5. Hermes maintains kanban + Telegram status with screenshots.

## Constraints
- AGENTS.md: redeploy `.10`, no commit of `docs/prints/`, Telegram group only.
- max-turns 200 for coding agents.
- One conventional commit per task.
- Do not block on PowerDNS/monitor not yet implemented — mark N/A in inventory.
