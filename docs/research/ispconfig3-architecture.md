# ISPConfig3 — Mapa Arquitetural (pesquisa para o port em Go)

> Nota: pesquisa interna em PT-BR. Raiz analisada: `base/ispconfig3_install/` (v3.3.1p1).

## 1. Arquitetura geral

Três camadas desacopladas **apenas via banco de dados**:

- **`interface/`** — painel web PHP. Ao salvar, `tform_actions` grava na tabela real E chama `db_mysql.inc.php::datalogSave/Insert/Update/Delete` (linhas 722-862), que serializa `{old, new}` (PHP `serialize`) e insere em **`sys_datalog`** com `server_id`, `dbtable`, `dbidx` (`campo:valor`), `action` (`i`/`u`/`d`). A interface NUNCA toca serviços do SO.
- **`server/`** — daemon disparado por cron (`server/server.php` via `cron.sh`/`server.sh`). Consumidor de `sys_datalog`.
- **`install/`** — instalador/atualizador; detecta distro, configura serviços, cria DB, renderiza configs de `install/tpl/`.

### Fluxo do daemon (`server/server.php`, 228 linhas)
1. Lockfile via PID em `temp/.ispconfig_lock`.
2. Config do server da tabela `server` (`config` = INI serializado, parseado por `ini_parser` → `$conf['serverconfig']`). Rescue-config em `temp/rescue_module_serverconfig.ser.txt`.
3. `$conf['last_datalog_id']` = `server.updated`.
4. Conta pendências: `sys_datalog WHERE datalog_id > last_datalog_id AND (server_id = ? OR server_id = 0)` (0 = broadcast).
5. Carrega `modules,plugins,file,services,system`; `modules->loadModules('all')`, `plugins->loadPlugins('all')`.
6. `raiseAction('server_plugins_loaded')`.
7. `modules->processDatalog()` → `processActions()` (tabela `sys_remoteaction`) → `services->processDelayedActions()` (restarts agrupados).
8. Remove lock.

### Eventos em dois níveis
**`modules.inc.php` (313 linhas):** `loadModules` varre `mods-enabled/`, instancia classe pelo nome do arquivo, `->onLoad()`. `registerTableHook($table,$module,$function)`. `processDatalog()`: SELECT LIMIT 1000, desserializa, `raiseTableHook(dbtable, action, data)`; faz replicação master→slave (REPLACE/DELETE local) em multi-server; atualiza `server.updated = datalog_id`.

**`plugins.inc.php` (178 linhas):** `loadPlugins` varre `plugins-enabled/` ordenado (ksort — prefixo `z_` controla ordem). `announceEvents($module,$events)`; `registerEvent` só aceita evento anunciado; `raiseEvent($event,$data)`. `registerAction/raiseAction` para `sys_remoteaction`.

**Cadeia:** `sys_datalog` row → `processDatalog` → `raiseTableHook('dns_soa','u',data)` → módulo traduz para evento nomeado → `raiseEvent('dns_soa_update',data)` → plugin (`bind_plugin::soa_update`) renderiza template, escreve config, agenda restart.

**Módulos** (`mods-available/`): web, dns, mail, client, cron, database, monitor_core, remoteaction_core, rescue_core, server, vm, xmpp, extension.

## 2. Banco de dados

Schema: `install/sql/ispconfig3.sql` (117KB, ~80 tabelas). Também `powerdns.sql`, `incremental/`. Toda tabela de dados de usuário tem: `sys_userid, sys_groupid, sys_perm_user, sys_perm_group, sys_perm_other` (strings `riud`).

- **SYS:** sys_user, sys_group, sys_datalog, sys_remoteaction, sys_config, sys_ini, sys_dbsync, sys_filesync, sys_cron, sys_log, sys_session, sys_theme, sys_message.
- **SERVER:** server (config INI serializado), server_ip, server_ip_map, server_php.
- **CLIENT:** client, client_template, client_template_assigned, client_circle, client_message_template, country.
- **WEB:** web_domain, web_database, web_database_user, web_folder, web_folder_user, web_backup, web_traffic, webdav_user, shell_user, ftp_user, ftp_traffic, cron, directive_snippets.
- **DNS:** dns_soa, dns_rr, dns_slave, dns_template, dns_ssl_ca.
- **MAIL:** mail_domain, mail_user, mail_forwarding, mail_get, mail_transport, mail_access, mail_content_filter, mail_relay_domain, mail_relay_recipient, mail_mailinglist, mail_user_filter, mail_traffic, mail_backup, spamfilter_policy, spamfilter_users, spamfilter_wblist.
- **Outros:** iptables/firewall, xmpp_*, openvz_*, aps_*, monitor_data, remote_user, remote_session, attempts_login, support_message, help_faq.

### web_domain (principais colunas)
domain_id (PK), server_id, ip_address, ipv6_address, domain, type (vhost/vhostsubdomain/vhostalias), parent_domain_id, vhost_type, document_root, web_folder, system_user, system_group, hd_quota, traffic_quota, cgi/ssi/suexec, errordocs, subdomain, php (no/fast-cgi/cgi/mod/php-fpm/hhvm), redirect_type, redirect_path, seo_redirect, rewrite_to_https, ssl, ssl_letsencrypt, ssl_letsencrypt_exclude, ssl_state/locality/organisation/organisation_unit/country/domain, ssl_request, ssl_cert, ssl_bundle, ssl_key, ssl_action, stats_password, stats_type, allow_override, apache_directives, nginx_directives, proxy_directives, php_fpm_use_socket, php_fpm_chroot, pm (dynamic/static/ondemand), pm_max_children/start_servers/min_spare/max_spare/process_idle_timeout/max_requests, php_open_basedir, custom_php_ini, backup_*, active, traffic_quota_lock, rewrite_rules, added_date, added_by, directive_snippets_id, enable_pagespeed, http_port, https_port, folder_directive_snippets, log_retention, proxy_protocol, server_php_id, jailkit_*, disable_symlinknotowner.

### dns_soa
id, server_id, origin (FQDN com ponto final), ns, mbox, serial, refresh, retry, expire, minimum, ttl, active, xfer, also_notify, update_acl, dnssec_initialized (Y/N), dnssec_wanted (Y/N), dnssec_algo (SET: NSEC3RSASHA1, ECDSAP256SHA256), dnssec_last_signed, dnssec_info, rendered_zone (cache da zona).

### dns_rr
id, server_id, zone (FK→dns_soa.id), name, type (A, AAAA, ALIAS, CNAME, MX, NS, TXT, PTR, SRV, SPF, CAA, DS, TLSA, SSHFP, NAPTR, HINFO, RP, LOC, DNAME…), data, aux (prioridade), ttl, active (Y/N), stamp, serial.

### server
server_id, server_name, flags tinyint(1): mail_server, web_server, dns_server, file_server, db_server, vserver_server, proxy_server, firewall_server, xmpp_server; config (INI multi-seção), updated (último datalog_id processado), mirror_server_id, dbversion, active.

## 3. Módulo web/nginx — `nginx_plugin.inc.php` (3627 linhas)

**Eventos:** web_domain_insert/update/delete (2 handlers cada: `ssl` e insert/update/delete), server_ip_*, webdav_user_*, client_delete, web_folder_user_*, web_folder_update/delete.

**Funções-chave:**
- `ssl()` (96): CSR/cert self-signed via openssl; lê/grava ssl_request/cert/key; `ssl_action` create/save/del; rejeita `.acme.invalid`.
- `insert()/update()` (313/324): cria dirs (document_root, web/, log/, tmp/, ssl/, webdav/, private/, cgi-bin/), usuário/grupo SO, quota (`setquota`/xfs), symlinks (pma, webmail), stats (awstats/webalizer/goaccess), renderiza vhost.
- `delete()` (2122): remove tudo.
- `php_fpm_pool_update/delete()` (2878/3099): pool de `php_fpm_pool.conf.master`; usa web_domain.pm*, server_php; socket ou TCP.
- `nginx_merge_locations()` (3174): merge de blocos `location` custom com template.
- `get_seo_redirects()` (3419), `url_is_local()` (3357).
- Jailkit: `_setup_jailkit_chroot()` (3465) etc.
- `_create_web_folder_auth_configuration()` (2623): HTTP basic auth por pasta.

**Let's Encrypt** (`letsencrypt.inc.php`, 932 linhas): cliente preferido **acme.sh** (webroot `/usr/local/ispconfig/interface/acme`, ECDSA ec-256 ou RSA 4096), fallback **certbot**; `use_acme()` decide; reload por server_type.

**Templates** (`server/conf/*.master`): nginx_vhost.conf.master (engine `<tmpl_var>/<tmpl_if>/<tmpl_loop>`), php_fpm_pool.conf.master, apps_php_fpm_pool.conf.master, nginx_apps.vhost.master, nginx_reverse_proxy_plugin.vhost.conf.master, nginx_http_authentication.auth.master, php-fcgi-starter.master, awstats_index.php.master, goaccess_index.php.master.

## 4. Módulo DNS/Bind — `bind_plugin.inc.php` (642 linhas)

**Eventos:** dns_soa_*, dns_slave_*, dns_rr_*.

- `soa_update()` (278): config via `getconf->get_server_config(server_id,'dns')`; SELECT `dns_rr WHERE zone=? AND active='Y'`; renderiza `bind_pri.domain.master`. TTL 0 → vazio, nome vazio → `@`, split TXT >255, CAA → TYPE257 hex em BIND <9.9.6. Escreve zonefile em `bind_zonefiles_dir/<masterprefix><domínio>`; chown bind_user/group; salva cópia em `dns_soa.rendered_zone`. Valida com `named-checkzone`; em erro restaura o antigo e renomeia novo para `.err` + `datalogError`.
- `rr_*` (522-563): buscam SOA pai e re-disparam `soa_update` (regenera zona inteira).
- `write_named_conf()` (567): reconstrói `named.conf.local`: SELECT `dns_soa WHERE active='Y' AND server_id=?`; allow-transfer/also-notify/allow-update de xfer/also_notify/update_acl (vírgula→;); zonas dnssec_wanted='Y' apontam para `.signed`. Templates: `bind_named.conf.local.master` + `.slave`.
- **DNSSEC** (80-268): `soa_dnssec_create` (ZSK+KSK via dnssec-keygen; alg 13 ECDSAP256SHA256 ou alg 7; entropia >200), `soa_dnssec_sign` ($INCLUDE keys, dnssec-signzone 16 dias, DS/DNSKEY → dnssec_info), `soa_dnssec_update/delete`.
- Restart: `restartServiceDelayed('bind','restart'|'reload')` — restart se update_acl, senão reload.

## 5. Interface — `interface/web/`

**Módulos:** admin, client, dns, sites, mail, mailuser, monitor, dashboard, vm, tools, help, login, remote, themes, js. Padrão por módulo: `<entidade>_edit.php`/`_list.php`/`_del.php` + `form/*.tform.php` + `list/*.list.php` + `templates/*.htm` + `lib/lang/`.

**Forms (tform)** — ex. `dns/form/dns_soa.tform.php`: `$form` meta (title, db_table, db_table_idx, db_history yes→sys_datalog, tab_default, list_default, auth); `auth_preset` (userid, groupid, perm_user/group/other riud); `tabs` com fields (datatype INTEGER/VARCHAR/TEXT/DATE; formtype TEXT/SELECT/CHECKBOX/PASSWORD; default; value; **validators**: REGEX, UNIQUE, NOTEMPTY, ISEMAIL, ISINT, ISPOSITIVE, ISIPV4, ISIPV6, ISIP, CUSTOM) e plugins (ex. plugin_listview). Motor: `tform.inc.php` + `tform_base.inc.php` + `tform_actions.inc.php`. 25 forms DNS por tipo de record. Listas: `listform.inc.php` + `list/*.list.php`.

**Templates:** `.htm` com engine própria `tpl.inc.php` (mesma sintaxe tmpl_var/tmpl_if/tmpl_loop do server).

**Tradução:** `$wb[...]` em `<módulo>/lib/lang/<locale>_<form>.lng`.

**API remota:** `remoting.inc.php` + handlers soap/rest/json + `remote.d/`: dns, sites, mail, client, admin, server, domains, monitor... Ex.: dns_zone_add/update/delete/get, dns_rr_*, dns_a_add. Auth por remote_user/remote_session, permissões em `remote_user.remote_functions`.

**Segurança de entrada:** PHPIDS (`ids.inc.php`), IDN em `idn/`.

## 6. Regras / convenções

**CONTRIBUTING.md:** não refatorar core; PHP 5.4+; tabelas/colunas lowercase; métodos camelCase; vars snake_case; tabs (4); config interface via INI (`system.ini.master` → `get_global_config`), config server via `server.ini.master` → `get_server_config($server_id,'web'|'dns')`; traduções em todas as línguas.

**Permissões (crítico):** sys_userid, sys_groupid + sys_perm_user/group/other com flags r/i/u/d estilo Unix. Admin = sys_user id 1.

**security/:** `security_settings.ini` — flags yes/no/superadmin para allow_shell_user, admin_allow_server_config, admin_allow_remote_users, remote_api_allowed etc. `nginx_directives.blacklist` / `apache_directives.blacklist`: diretivas proibidas em campos custom (previne RCE).

## 7. Installer — `install/`

`install.php`; `installer_base.lib.php` (~66 funções) + `install.lib.php` + `mysql.lib.php`. Update: `update.php`.

- Detecção de distro: `install/dist/conf/<distro>.conf.php` (paths por distro) + `install/dist/lib/<distro>.lib.php` (overrides configure_*).
- DB: configure_database, install_ispconfig (cria `dbispconfig`, importa ispconfig3.sql), add_database_server_record, detect_ips.
- configure_*: postfix, dovecot, amavis, rspamd, bind, powerdns, apache, nginx, pureftpd, jailkit, ufw_firewall, vlogger, apps_vhost, install_crontab, install_acme...
- Templates em `install/tpl/*.master` (124). `setrights.php` pós-instalação.
- Distros (`install/dist/conf/`): Debian 4–13+testing; Ubuntu 16.04–24.04; CentOS 5–9; Fedora; openSUSE; Gentoo.

## Notas para o port em Go

- Acoplamento 100% via banco — `sys_datalog` é o eixo (produtor na API, consumidor no daemon). `data` é PHP-serialize de `{old,new}`.
- Dois registries: table-hooks (módulos) e events (plugins).
- Templates `.master`: engine própria, não Twig — precisa de renderer compatível.
- Config de server/interface é INI serializado em colunas (`server.config`, `sys_ini.config`).
- Permissões riud devem ser portadas fielmente.

Arquivos-âncora: `server/server.php`, `server/lib/classes/{modules,plugins,services,getconf,tpl}.inc.php`, `server/plugins-available/{nginx,bind}_plugin.inc.php`, `server/mods-available/{web,dns}_module.inc.php`, `install/sql/ispconfig3.sql`, `interface/lib/classes/{db_mysql,tform,tform_actions,remoting}.inc.php`, `interface/lib/classes/remote.d/*.inc.php`, `install/lib/installer_base.lib.php`.
