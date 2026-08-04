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
docker run -d --name redis-ispconfig -p 6379:6379 redis:7-alpine   # task queue asynq (D12)

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

- Swagger não pode ficar stale: `make swagger` após mudar handlers (regra completa abaixo).
- Commits convencionais (`feat:`, `fix:`, `docs:`...). Cada task terminada e validada = 1 commit.
- NUNCA commitar: `docs/prints/`, `base/`, `.vagrant/`, `*.box`, `testdata/`, `config.toml` real, senhas/credenciais/dados sensíveis, imagens Vagrant.
- `docs/screenshots/` (curados) SOBE no repo; `docs/prints/` (validação local) NÃO sobe.

## Swagger (obrigatório em TODA mudança de API)

**Toda vez que adicionar uma nova API ou ajustar uma existente, atualize o Swagger.**
Sem exceção — endpoint novo, rota renomeada, campo novo no body, novo código de
erro, mudança de auth/escopo, ou até só o texto de uma `@Description`.

```bash
make swagger        # swag fmt + swag init → internal/api/docs (go + json)
make swagger-check  # falha se o spec commitado estiver stale — é o que o CI roda
```

Checklist ao mexer em `internal/api/`:

1. Anotar o handler com swaggo: `@Summary`, `@Description`, `@Tags`, `@Param`,
   `@Success`, `@Failure` (todos os códigos que o handler realmente retorna) e
   `@Router`, mais `@Security CookieAuth` / `@Security BearerAuth`.
2. Rodar `make swagger` e **commitar** `internal/api/docs/docs.go` +
   `swagger.json` no mesmo commit da mudança de API — nunca num commit separado.
3. Conferir `make swagger-check` limpo antes do `git push`.
4. Rotas registradas genericamente (`RegisterEntity`) não geram anotação
   sozinhas: adicione as funções `*Doc()` correspondentes, como
   `internal/api/serverip.go` faz.
5. Se a mudança altera como o cliente autentica ou o que ele precisa para
   chamar o endpoint, atualize também `docs/api-tokens.md` e a descrição do
   `BearerAuth` — o spec é a documentação que o usuário externo lê.

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

## Servidor legado real (validação de migração)

Existe um servidor ISPConfig3 PHP **real** (Apache2 + PHP-FPM) disponível como base
de leitura para validação de migração e testes da API legada — acessos e regras em
`AGENTS.local.md` (**não commitado**; nunca copiar credenciais para arquivos
versionados). Regra absoluta: **somente leitura** — inventário, API remota,
agent-browser e SELECTs/dumps para análise local; nada de escrita. Se a VPN cair:
`nmcli connection up VPN_Criare`.

**Testes de escrita** (criação de entidades, imports, migrate-from apply, CRUD
de módulos) usam SEMPRE o lab local em `vagrant/lab/README.md` — duas VMs
ISPConfig3 completas (nginx `.20`, apache2 `.21`, mail + Roundcube, remote API
com usuário full-grant `goisp-lab`), dataset de fixtures idempotente
(`make vagrant-lab-fixtures`) — NUNCA o servidor real.

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
| Log de debug | `LOG_LEVEL=debug` (env, sem rebuild) ou `[log] level` no config.toml — vale para `serve` e `daemon` |
| **Lab painel `.10`** | **Redeploy do binário na VM go-ispconfig (ver abaixo) — obrigatório em todo marco de módulo/UI** |

## Redeploy na VM lab `192.168.56.12` (obrigatório)

E2E local (`127.0.0.1:809x`) **não substitui** validação no painel lab final.
A cada marco que altera o binário embutido (módulo fechado, lote UI/API relevante, archive de change, ou pedido explícito do usuário), o agent DEVE redeployar e checar saúde na VM:

```bash
# A partir do worktree/clone com o código a validar:
make build-linux
cd vagrant
vagrant upload ../bin/go-ispconfig-linux-amd64 /tmp/go-ispconfig debian
vagrant ssh debian -c 'sudo install -m 0755 /tmp/go-ispconfig /usr/local/bin/go-ispconfig && sudo systemctl restart go-ispconfig-serve go-ispconfig-daemon && systemctl is-active go-ispconfig-serve go-ispconfig-daemon && /usr/local/bin/go-ispconfig version'
# Health
curl -sk -o /dev/null -w '%{http_code}\n' https://192.168.56.12:8080/
```

### Topologia do lab (atual)

| VM Vagrant | IP | Papel |
|---|---|---|
| `debian` | **192.168.56.12** (não `.11` do Vagrantfile) | painel go-ispconfig de **teste** — dataset E2E completo, é onde se valida |
| `legacy` | **192.168.56.20** | ISPConfig3 PHP — baseline de **paridade** e fonte de migração |

As VMs `ubuntu` (.10), `apache-test` (.22) e `legacy-apache` (.21) foram destruídas;
recrie com `cd vagrant && vagrant up <nome>` quando precisar testar instalação do zero.

- Preferir `vagrant ssh debian` (não `ssh root@192.168.56.12` com chave solta).
- Incluir no relatório/Telegram: commit/version deployado + HTTP code do painel.
- Se a VM estiver down: `cd vagrant && vagrant up debian` antes do upload — não pular o redeploy em silêncio.

## Status ao usuário (Telegram + e-mail) — obrigatório a cada marco

A cada marco do loop de implementação (task concluída, lote fechado, change finalizada, contingência
de quota, transbordo para agent externo, falha), **além** do e-mail de status
(`scripts/send-status-email.py`), o agente DEVE notificar o usuário via **Hermes MCP bridge**
para o Telegram. Não substituir o e-mail — adicionar o Telegram.

### Destino

Apenas no **grupo** `telegram:-1001683954128` (Nilton Hermes Agent). **Não enviar na DM**
(`telegram:75440030`) — o grupo é a fonte única de updates do projeto.

### Cabeçalho (em negrito)

A primeira linha da mensagem deve identificar quem está enviando, em negrito (Markdown Telegram):

```
*github:* jniltinho/go-ispconfig   *cli:* claude
*projeto:* go-ispconfig   *pasta:* /home/nilton/Projetos/nilton/go-ispconfig
```

- `*github:*` — owner/repo REAL do remote (`git remote -v`), hoje
  `jniltinho/go-ispconfig`. Se estiver em outro repo, ajustar.
- `*cli:*` — o CLI em uso, minúsculo: `claude`, `grok`, `codex`, `agent`
  ou `opencode` (para opencode, pode anotar o modelo: `opencode (kimi-k3)`,
  `opencode (minimax-m3)`).
- `*projeto:*` — nome do projeto (diretório raiz do repo).
- `*pasta:*` — caminho absoluto onde o projeto está executando.

### MCP server

O Claude Code já tem o servidor `hermes` configurado localmente (`claude mcp list` → `hermes ✔ Connected`,
comando `hermes mcp serve`). Tools disponíveis (sem precisar de aprovação):

- `channels_list` — descobre os targets Telegram (use para confirmar antes do primeiro envio)
- `messages_send(target="telegram:<chat_id>", message="...")` — manda a mensagem
- `events_poll` / `events_wait` — se quiser ouvir respostas (não obrigatório)

**Importante:** chamar `messages_send` **apenas uma vez por marco**, com `target="telegram:-1001683954128"`.

### Formato da mensagem

Markdown Telegram. Curto: 1–6 linhas após o cabeçalho. Esquema sugerido:

```
*github:* jniltinho/go-ispconfig   *cli:* <cli em uso>
*projeto:* go-ispconfig   *pasta:* <caminho absoluto do repo>
🔔 <emoji_marco> <change-id> • <task/lote>
<resumo em uma linha>
• resultado: <OK / parcial / falhou — com motivo curto>
• próxima: <próxima ação concreta>
```

Emojis por marco: 🟢 concluído / 🟡 parcial / 🔴 falhou / 🚀 change despachada / 📦 commit / 👀 aguardando revisão.

### Anexos de UI (obrigatório quando o frontend mudar)

Se o marco **alterou ou criou interface** (Vue routes/views/components, tema, formulários, nav, telas novas), o resumo no Telegram **NÃO pode ser só texto** — tem que ir **com imagens**:

1. Capturar screenshots (agent-browser ou painel lab `.10` / E2E local) em `docs/prints/` (nunca commitados).
2. Preferir 1–4 frames úteis: lista, form vazio, form preenchido, estado pós-save; light e dark se o tema mudou.
3. Enviar no **mesmo marco** do status:
   - **Hermes (esta sessão / MCP):** incluir `MEDIA:/caminho/absoluto/arquivo.png` na mensagem (ou um envio por imagem se o bridge limitar).
   - **Claude Code via `messages_send`:** se a tool aceitar attachment/path, anexar; senão chamar Hermes/`hermes send` com as mídias **logo após** o texto do marco (mesmo grupo `telegram:-1001683954128`).
4. No corpo do texto, citar o que a imagem mostra (ex.: “System → Firewall — list + form”).
5. Se a UI **não** mudou (só backend/daemon/docs), imagens são opcionais.

Marcos que **sempre** exigem imagem: tasks de UI (ex. 4.x), tema, E2E de painel aprovado, redeploy na `.10` que estreia tela nova.

### Exemplos

Marco de task única (Claude Code):

```
*github:* jniltinho/go-ispconfig   *cli:* claude
*projeto:* go-ispconfig   *pasta:* /home/nilton/Projetos/nilton/go-ispconfig
🟢 add-client-module • task 17/23 — reseller sync
sync entre `client` e `sys_user` validado em paridade; `c7ff541` commitado.
• próxima: task 18/23 (sys_datalog audit fields).
```

Lote fechado (opencode):

```
*github:* jniltinho/go-ispconfig   *cli:* opencode (kimi-k3)
*projeto:* go-ispconfig   *pasta:* /home/nilton/Projetos/nilton/go-ispconfig
📦 add-mail-module • batch 1 (3 tasks)
DKIM keypair + Rspamd maps + Postfix transport stub. revisão cruzada ok.
• próxima: batch 2 (vmail mailbox quota + sieve defaults).
```

Contingência de quota (Claude Code despachando para MiniMax-M3):

```
*github:* jniltinho/go-ispconfig   *cli:* claude
*projeto:* go-ispconfig   *pasta:* /home/nilton/Projetos/nilton/go-ispconfig
🔴 add-mail-module • contingência de quota
API Claude estourou (reset previsto ~13:20). Despachando tasks 14–17
para MiniMax-M3 via opencode; e-mail de status enviado.
```

### Regra operacional

- Não acumular updates — mandar **logo após** o commit / lote / falha.
- Se a chamada `messages_send` falhar (tool error), ainda enviar o e-mail (e vice-versa).
- Citar o SHA curto do commit quando houver.
- **Nunca** mandar na DM `75440030` — só no grupo.

## Atualizar documentação (mudanças grandes)

| O quê mudou | Onde documentar |
|---|---|
| Arquitetura/fluxo datalog | `docs/ARCHITECTURE.md` |
| Endpoint/API | annotations swaggo + `make swagger` |
| Instalação/migração | `README.md` + `docs/MIGRATION.md` |
| Decisão de design | change OpenSpec correspondente (`design.md`) |
| Task concluída | marcar `- [x]` no `tasks.md` da change + commit |
