# ISPConfig3 — Análise do Tema Visual (pesquisa para recriação em Vue 3 + Tailwind v4)

> Base: `base/ispconfig3_install/interface/web/themes/default/` — jQuery 2.1 + Bootstrap 3.3.0 + SASS. Um único tema (`default`), com esquema de cor claro (padrão) e escuro (compilado mas não ligado à UI — usar como guia para dark-mode futuro).

## Design tokens (esquema claro — `assets/stylesheets/themes/default/colors.sass`)

| Token | HEX | Uso |
|---|---|---|
| text principal | **#3C444B** | cinza-azulado escuro |
| fundo body | **#F2F5F7** | + linhas ímpares de tabela (zebra) |
| superfície | **#FFFFFF** | cards/tabelas/sidebar |
| **marca (vermelho ISPConfig)** | **#C70F19** | logout, hover de nav ativa; favicon/mask usa #cc151c |
| vermelho escuro | ~#9C0C14 | borda inferior botão logout |
| verde (sucesso/ativo) | **#3CB355** | classe `.green` |
| gradiente verde botões | #7BCC89 → #6AB977 | `formbutton-success`; borda #5AAB68 |
| gradiente cinza | #FFFFFF → #EEF0F2 | botões default, tfooter, nav |
| borda neutra | **#D3D7DA** | tabelas/cards; #CCCCCC borda inferior de botões |
| fundo dashlets | #E1E4E9 | cards de módulo |
| info bg | **#DFEAF6** | alerts info, header sidebar, hover de linha |
| info border / font | #CEDDED / #698296 | |
| **link** | **#2371CA** | |
| danger bg / border / font | **#F7DFDF** / #DCB2B3 / #95686B | alerts de erro |
| thead escuro | #57646D → #3E474E | gradiente do cabeçalho de tabela |

Sem cor "warning" própria; login usa alert-success do BS3.

**Tipografia:** stack de sistema (Helvetica Neue/Arial), base 14px/1.428. Sem webfont de texto (zero CDN). Único @font-face: icon-font `ispconfig` local. Caption tabela 18px bold; título módulo 16px bold; botões 12px bold; footer 10px; ícones nav 32px.

**Bordas:** hoje `border-radius: 4px` consistente → **zerar (`--radius: 0`) no novo tema** (requisito: cantos quadrados).

**Espaçamentos:** painel fixo 950px (conteúdo 710px + sidebar 215px) → modernizar para fluido/max-w. Sidebar li padding 10px; caption 5px 10px; tab-content 25px 10px; botões 6px 30px; seções ~15-20px.

**Sombras:** quase só inset, sutis (`inset 1px 3px 8px -5px rgba(0,0,0,.2)` nos cards). Visual flat com leve profundidade interna. `text-shadow: 1px 1px 1px white` nos ícones da nav.

**Assinatura de botões:** borda inferior 2px mais escura + gradiente vertical + transition 500ms. Variantes: default (cinza), success (verde), danger/logout (vermelho).

## Layout do painel (`main.tpl.htm`)

1. **Header:** logo 200×65 (base64 injetado — white-label) à esquerda; à direita botão logout vermelho ("Logout <usuário>") + busca global (campo + lupa `icon-lens`).
2. **Top-nav** (`btn-group-justified`, altura 70px): um botão por módulo — **ícone 32px em cima + título bold embaixo**. Módulos: Dashboard, Sites, DNS, Mail, Client, Monitor, System/Admin, Tools, Help, VM. Ativo = `.active` com hover vermelho.
3. **Conteúdo:** `#content` (710px, dir.) com form `#pageForm`/`#pageContent` carregado via AJAX (SPA-like em jQuery); `#sidebar` (215px, esq.) card de notícias com header azul-claro.
4. **Footer:** borda-topo, centralizado, 10px.

Off-canvas mobile (Pushy). Breakpoints: 970/860/670/600 (tabelas viram cards empilhados)/350px.

## Componentes

**Datatable (`*_list.htm`):** `.page-header > h1` + descrição; botão verde "Add new record"; tabela com `thead.dark` (linha 1 = títulos, **linha 2 = filtros inline por coluna** + botão `icon-filter` — traço marcante); colunas ID/Active/Server/Domain + **ações à direita** (botões-ícone estreitos: link externo, stats, delete vermelho com confirm); zebra ímpar #F2F5F7, hover #DFEAF6, `.danger` para inativos; `tfoot` com paginação server-side; densidade média (padding célula ~8px, thead 40px).

**Forms com abas (`tabbed_form.tpl.htm`):** card branco com `nav-tabs` **sem raio, divisória vertical 1px, aba ativa branca**; conteúdo padding 25px 10px; `.form-horizontal` labels à esquerda com **`:after {content: ':'}`**; `fieldset-legend` como sub-cabeçalhos; CSRF hidden; Save/Cancel.

**Alerts:** `.alert-notification` azul #DFEAF6; `.alert-danger` #F7DFDF com `.alert-label` ("Error", 60px) + lista de erros. Modal BS3 de datalog pendente no header (`#datalogModal`).

**Login:** centralizado flex, col-md-4, panel BS3 com heading gradiente + logo; form username/password + "stay logged in" + botão à direita + "password lost".

**Dashboard (dashlets):** `ul.modules` — cards 200px, fundo #E1E4E9, ícone 50px + título 16px + botão full-width.

## Iconografia

Icon-font própria **`ispconfig`** (local, `assets/fonts/ispconfig.*`), codepoints PUA: sites \e604, mail \e608, dns \e609, client \e60a, monitor \e605, admin \e603, tools \e602, dashboard \e606, help \e607, billing \e60d, vm \e601/\e600, lens \e60b, filter \e614, edit \e615, delete \e613, action \e610, link \e60f, loginas \e611, dbadmin \e612, calendar \e60e, bulb \e60c. + Font Awesome 4.7 local + Glyphicons BS3 (ex. `glyphicon-signal` stats). Flags sprite p/ idiomas. Favicon set completo (#cc151c).

Libs UI no bundle: Select2, Bootstrap Datetimepicker, Pushy, Tipsy, Chart.js, Modernizr.

## Resumo para o novo tema (Vue 3 + Tailwind v4, "mesma cara, mais bonito")

- Tokens: `--color-brand: #C70F19`; bg `#F2F5F7`; surface `#FFF`; text `#3C444B`; link `#2371CA`; success `#3CB355`; border `#D3D7DA`; info `#DFEAF6`; danger `#F7DFDF`; thead `#3E474E`. **`--radius: 0`**.
- Layout: topbar (logo + módulos ícone-sobre-título + logout vermelho + busca) → conteúdo fluido; sidebar por seção.
- Ícones: substituir icon-font por Lucide (mapear 1:1 os módulos acima).
- Tabelas: cabeçalho escuro, filtro inline no thead, ações à direita, zebra, paginação no rodapé — manter densidade.
- Forms: abas retas no topo do card, labels com `:`, botões sólidos modernos (abandonar gradiente/borda-base 2px).
- Modernizações: largura fluida, dark-mode preparado (esquema dark existe como referência), sombras externas sutis em vez de inset, transitions curtas.
