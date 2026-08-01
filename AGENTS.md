# AGENTS.md — go-ispconfig

Runbook para qualquer IA/agent trabalhar neste projeto: subir ambiente, buildar, testar, validar.
Projeto: port do ISPConfig3 (PHP) para Go — painel de hospedagem com nginx (web) e Bind (DNS).

## O básico que todo agent precisa saber

- **Módulo/binário**: `go-ispconfig` (module name simples, sem path GitHub — padrão dos projetos deste autor, ver go-cubemail).
- **Stack**: Go + Echo v5 + GORM (MariaDB) + Cobra + Viper (`config.toml`); frontend Vue 3 + Vite + TS + Tailwind v4 + Pinia, embedado no binário via `//go:embed all:web/dist`. Fonts locais (zero CDN). Cantos quadrados (`--radius: 0`).
- **Banco**: schema 100% idêntico ao ISPConfig3 (`ispconfig3.sql` embedado) — NUNCA alterar nomes/tipos de tabelas/colunas existentes; isso quebra a migração de clientes do ISPConfig PHP.
- **Arquitetura**: interface nunca toca o SO. API grava mudanças + diff em `sys_datalog`; o daemon (processo persistente com scheduler interno, sem cron do sistema) consome o datalog e aplica configs (nginx vhosts, zonas Bind) com validação (`nginx -t`, `named-checkzone`) e rollback.
- **Referências obrigatórias antes de mexer**:
  - `docs/research/ispconfig3-architecture.md` — arquitetura do ISPConfig3 original
  - `docs/research/ispconfig3-theme.md` — tema visual original e tokens do novo tema
  - `openspec/changes/` — propostas OpenSpec (fundação + módulos); implemente SEMPRE a partir da change correspondente (`/opsx:apply`)
  - Código PHP original em `base/ispconfig3_install/` (somente leitura, não commitado)
- **Idioma**: código, docs e UI em inglês; UI com i18n pronta (locale `en.json`).
- **Distros-alvo**: Debian 11–13, Ubuntu 22.04–24.04.

## Subir o ambiente

```bash
# Dependências: go >= 1.26, node >= 22, mariadb (local ou docker), vagrant+virtualbox (testes de instalação)
docker run -d --name mariadb-ispconfig -e MARIADB_ROOT_PASSWORD=root -p 3306:3306 mariadb:11

make all            # clean + frontend (npm run build → web/dist) + build do binário
./go-ispconfig init      # gera config.toml default
./go-ispconfig migrate   # cria schema (DDL idêntico ao ISPConfig3) + seed admin
./go-ispconfig serve     # painel + API + swagger em /swagger/
./go-ispconfig daemon    # daemon (datalog + scheduler) — precisa root p/ aplicar configs
```

Frontend em dev: `cd frontend && npm run dev` (proxy `/api` → :8080).

## Antes de `git push` (obrigatório)

```bash
golangci-lint run ./...
go build ./... && go vet ./...
go test ./internal/...
cd frontend && npm run build   # garante que web/dist compila
```

- Swagger não pode ficar stale: `make swagger` após mudar handlers.
- Commits convencionais (`feat:`, `fix:`, `docs:`...). Cada task terminada e validada = 1 commit.
- NUNCA commitar: `docs/prints/`, `base/`, `.vagrant/`, `*.box`, `testdata/`, `config.toml` real, senhas/credenciais/dados sensíveis, imagens Vagrant.
- `docs/screenshots/` (curados) SOBE no repo; `docs/prints/` (validação local) NÃO sobe.

## Validação cruzada com outros agents (obrigatório ao terminar cada task)

Toda task/código terminado deve ser validado pelos agents externos antes de ser considerado concluído.
Rode em paralelo sobre o diff (ou sobre os arquivos da task) e pergunte explicitamente:
**"isso precisa de refinamento, melhoria ou correção? responda objetivamente com uma lista"**

```bash
DIFF=$(git diff HEAD~1)  # ou git diff para trabalho não commitado

grok -p "Revise este código Go/Vue do go-ispconfig. Precisa de refinamento, melhoria ou correção? Liste objetivamente. $DIFF"

codex exec "Revise este diff do go-ispconfig quanto a bugs, segurança e simplificação. Precisa de refinamento, melhoria ou correção? git diff HEAD~1"

opencode run -m kimi-for-coding/k3 "Revise o diff atual (git diff HEAD~1) do go-ispconfig. Precisa de refinamento, melhoria ou correção? Liste."

opencode run -m minimax-coding-plan/MiniMax-M3 "Revise o diff atual (git diff HEAD~1) do go-ispconfig. Precisa de refinamento, melhoria ou correção? Liste."

agent -p "Revise o diff atual do go-ispconfig (git diff HEAD~1). Precisa de refinamento, melhoria ou correção? Liste."
```

Consolidar o feedback: aplicar correções procedentes, descartar falsos positivos com justificativa curta, e só então marcar a task como concluída no `tasks.md` da change OpenSpec.

## Testar e validar

| O quê | Como |
|---|---|
| Unit/integração Go | `go test ./internal/...` (integração MariaDB: `go test -tags=integration ./...`) |
| Golden files (templates .master) | `go test ./internal/mastertpl/...` — nunca aceitar diff sem entender |
| Permissões riud | suite dedicada — isolamento entre clientes é crítico, rodar sempre que tocar em repository/auth |
| Frontend E2E | agent-browser contra o binário buildado (login, navegação, CRUD) |
| Screenshots | agent-browser → `docs/prints/` (validação humana); aprovados → `docs/screenshots/` |
| Instalação | `make vagrant-up && make vagrant-test` (VM Ubuntu 24.04, roda `go-ispconfig install --yes` + smoke tests) |
| API manual | Swagger UI em `/swagger/` |

## Atualizar documentação (mudanças grandes)

| O quê mudou | Onde documentar |
|---|---|
| Arquitetura/fluxo datalog | `docs/ARCHITECTURE.md` |
| Endpoint/API | annotations swaggo + `make swagger` |
| Instalação/migração | `README.md` + `docs/MIGRATION.md` |
| Decisão de design | change OpenSpec correspondente (`design.md`) |
| Task concluída | marcar `- [x]` no `tasks.md` da change + commit |
