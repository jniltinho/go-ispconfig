package apache2

import (
	"fmt"
	"strings"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/mastertpl"
)

// vhostTemplate is the master template name for Apache vhosts (port of
// server/conf/vhost.conf.master; the <tmpl_hook> extension points were
// dropped because go-ispconfig has no plugin-hook system).
const vhostTemplate = "apache_vhost.conf.master"

// vhostInput carries everything one vhost render needs. Assembled by the
// plugin from the datalog row plus DB lookups, consumed by pure functions so
// the rendering stays testable without a database.
type vhostInput struct {
	cfg *getconf.WebConfig
	d   row // the web_domain row being rendered
	// aliases are the active child alias/subdomain rows.
	aliases []row
	// serverPHP is the resolved server_php row for a pinned PHP version,
	// nil for the server default.
	serverPHP row
	// apacheVersion is the running Apache version ("2.4.58"); "" disables
	// every version-gated template branch, which renders the 2.2 syntax, so
	// the plugin always probes before rendering.
	apacheVersion string
	// ips are the (ip, ssl) listen combinations of this site.
	ips []listener
	// sslOCSP reports whether the site certificate carries an OCSP URI.
	sslOCSP bool
	// sslBundle reports whether a non-empty chain file exists.
	sslBundle bool
}

// listener is one <VirtualHost ip:port> the site answers on.
type listener struct {
	ip    string
	port  int64
	ssl   bool
	proxy bool // PROXY protocol on this listener
}

// fpmInfo is the PHP-FPM addressing derived for a domain. Same derivation as
// the nginx plugin (pool web<domain_id>, TCP port php_fpm_start_port +
// domain_id - 1) so a server switched between web servers keeps its pools.
type fpmInfo struct {
	poolName   string
	poolDir    string
	socketDir  string
	socketPath string
	port       int64
	useSocket  bool
	initScript string
}

// resolveFPM derives the pool name, dir, socket and TCP port of a domain.
func resolveFPM(cfg *getconf.WebConfig, d row, serverPHP row) fpmInfo {
	poolDir, socketDir, initScript := cfg.PHPFPMPoolDir, cfg.PHPFPMSocketDir, cfg.PHPFPMInitScript
	if serverPHP != nil {
		if v := strings.TrimSpace(serverPHP.str("php_fpm_pool_dir")); v != "" {
			poolDir = v
		}
		if v := strings.TrimSpace(serverPHP.str("php_fpm_socket_dir")); v != "" {
			socketDir = v
		}
		if v := strings.TrimSpace(serverPHP.str("php_fpm_init_script")); v != "" {
			initScript = v
		}
	}
	info := fpmInfo{
		poolName:   fmt.Sprintf("web%d", d.num("domain_id")),
		poolDir:    strings.TrimSuffix(strings.TrimSpace(poolDir), "/"),
		socketDir:  strings.TrimSuffix(strings.TrimSpace(socketDir), "/"),
		useSocket:  d.str("php_fpm_use_socket") == "y",
		initScript: initScript,
	}
	info.socketPath = info.socketDir + "/" + info.poolName + ".sock"
	if start := atoi64(cfg.PHPFPMStartPort); start > 0 {
		info.port = start + d.num("domain_id") - 1
	}
	return info
}

// phpMode normalizes web_domain.php to a value the template branches on.
// hhvm is dead upstream and is treated as "no" (proposal non-goal).
func phpMode(v string) string {
	switch v {
	case "mod", "fast-cgi", "cgi", "suphp", "php-fpm", "no":
		return v
	case "hhvm", "":
		return "no"
	default:
		return "no"
	}
}

// allowOverride resolves the AllowOverride value: the site's own setting
// wins, falling back to htaccess_allow_override and then "All" (PHP default).
func allowOverride(cfg *getconf.WebConfig, d row) string {
	if v := strings.TrimSpace(d.str("allow_override")); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.HtaccessAllowOverride); v != "" {
		return v
	}
	return "All"
}

// docRoots returns the site document root, the served folder ("web" or the
// configured web_folder) and the "www" twin the template pairs it with.
func docRoots(d row) (documentRoot, served, www string) {
	documentRoot = strings.TrimSuffix(strings.TrimSpace(d.str("document_root")), "/")
	folder := "web"
	if d.str("type") == "vhostsubdomain" || d.str("type") == "vhostalias" {
		if f := strings.Trim(strings.TrimSpace(d.str("web_folder")), "/"); f != "" {
			folder = f
		}
	}
	served = documentRoot + "/" + folder
	// PHP renders the same path twice on Apache (the legacy /web pair); a
	// site with a custom web_folder still exposes /web as the www twin.
	www = documentRoot + "/web"
	return documentRoot, served, www
}

// buildVhost assembles the template variables and loops (port of the
// apache2_plugin update() vector building) plus the derived FPM addressing.
func buildVhost(in vhostInput) (map[string]any, map[string][]map[string]any, fpmInfo) {
	d, cfg := in.d, in.cfg
	fpm := resolveFPM(cfg, d, in.serverPHP)
	documentRoot, served, www := docRoots(d)
	php := phpMode(d.str("php"))
	sslOn := d.str("ssl") == "y" && d.str("ssl_domain") != ""

	vars := map[string]any{
		"domain":                  d.str("domain"),
		"web_basedir":             strings.TrimSuffix(cfg.WebsiteBasedir, "/"),
		"document_root":           documentRoot,
		"web_document_root":       served,
		"web_document_root_www":   www,
		"system_user":             d.str("system_user"),
		"system_group":            d.str("system_group"),
		"suexec":                  d.str("suexec"),
		"security_level":          cfg.SecurityLevel,
		"php_open_basedir":        d.str("php_open_basedir"),
		"allow_override":          allowOverride(cfg, d),
		"ssi":                     d.str("ssi"),
		"disable_symlinknotowner": yn(cfg.WebsiteSymlinks == "" && false),
		"logging":                 loggingMode(d),
		"errordocs":               d.str("errordocs"),
		"apache_version":          in.apacheVersion,
		"apache_full_version":     in.apacheVersion,
		"php":                     php,
		"apache_directives":       apacheDirectives(d),
		// mod_ruby / mod_perl / mod_python are rendered for template parity
		// only; go-ispconfig never provisions them (proposal non-goal).
		"ruby":   "n",
		"perl":   "n",
		"python": "n",
		// WebDAV and chroot are out of scope; the template keeps its markers.
		"php_fpm_chroot":            "n",
		"php_fpm_chroot_web_folder": "",
		"use_proxy_protocol":        "n",
		"custom_php_ini_dir":        customPHPIniDir(d),
		"has_custom_php_ini":        strings.TrimSpace(d.str("custom_php_ini")) != "",
		"custom_sendmail_path":      "",
		"fastcgi_config_syntax":     "2",
		"fastcgi_max_requests":      "5000",
		"fastcgi_starter_path":      documentRoot + "/cgi-bin/",
		"fastcgi_starter_script":    ".php-fcgi-starter",
		"cgi_starter_path":          documentRoot + "/cgi-bin/",
		"cgi_starter_script":        ".php-cgi-starter",
		"ssl_enabled":               sslOn,
		"has_bundle_cert":           in.sslBundle,
		"ssl_ocsp_supported":        in.sslOCSP,
		"rewrite_to_https":          d.str("rewrite_to_https"),
	}
	vars["ssl_crt_file"], vars["ssl_key_file"], vars["ssl_bundle_file"] = sslPaths(d)

	// PHP-FPM addressing: exactly one of use_socket/use_tcp is truthy so the
	// template emits one SetHandler block, never both.
	vars["use_socket"] = php == "php-fpm" && fpm.useSocket
	vars["use_tcp"] = php == "php-fpm" && !fpm.useSocket
	vars["fpm_socket"] = fpm.socketPath
	vars["fpm_port"] = fpm.port

	loops := map[string][]map[string]any{}
	loops["vhosts"] = listenerRows(in)
	buildAliases(in, vars)
	loops["redirects"] = redirectRows(d)
	loops["alias_seo_redirects"] = nil
	buildSEO(in, vars)
	vars["rewrite_enabled"] = len(loops["redirects"]) > 0 ||
		len(loops["alias_seo_redirects"]) > 0 ||
		vars["seo_redirect_enabled"] == true ||
		d.str("rewrite_to_https") == "y"
	return vars, loops, fpm
}

// loggingMode maps web_domain.traffic_quota_lock / stats settings to the
// template's logging value. ISPConfig stores it directly in web_domain.log_retention
// era columns; the modern column is "log_retention" with a separate flag.
func loggingMode(d row) string {
	switch d.str("logging") {
	case "no", "none":
		return "no"
	case "anon":
		return "anon"
	default:
		return "yes"
	}
}

// apacheDirectives returns the site's custom directives with CRLF normalized.
// Unlike nginx there is no directive blacklist in the PHP plugin: Apache
// directives are already constrained by the vhost context.
func apacheDirectives(d row) string {
	s := strings.ReplaceAll(d.str("apache_directives"), "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// customPHPIniDir is the per-site php.ini directory used by the suphp/cgi
// modes ("" when the site has no custom php.ini).
func customPHPIniDir(d row) string {
	if strings.TrimSpace(d.str("custom_php_ini")) == "" {
		return ""
	}
	return fmt.Sprintf("/usr/local/ispconfig/server/conf/php-ini/web%d", d.num("domain_id"))
}

// listenerRows builds the vhosts loop: one row per (ip, port) the site
// answers on. A site with SSL gets both the plain and the 443 listener; the
// template renders the SSL directives only on the ssl_enabled rows.
func listenerRows(in vhostInput) []map[string]any {
	rows := make([]map[string]any, 0, len(in.ips))
	for _, l := range in.ips {
		rows = append(rows, map[string]any{
			"ip_address":         l.ip,
			"port":               l.port,
			"ssl_enabled":        l.ssl,
			"use_proxy_protocol": yn(l.proxy),
		})
	}
	return rows
}

// defaultListeners is the listen set of a site with no explicit server_ip
// rows: the Apache wildcard on 80, plus 443 when SSL is on.
func defaultListeners(d row, sslOn bool) []listener {
	ip := strings.TrimSpace(d.str("ip_address"))
	if ip == "" || ip == "*" {
		ip = "*"
	}
	ls := []listener{{ip: ip, port: 80}}
	if sslOn {
		ls = append(ls, listener{ip: ip, port: 443, ssl: true})
	}
	return ls
}

// buildAliases renders the ServerAlias line(s). Apache accepts many names per
// directive; PHP batches them so a single directive never grows unbounded.
func buildAliases(in vhostInput, vars map[string]any) {
	var names []string
	if in.d.str("subdomain") == "www" || in.d.str("subdomain") == "*" {
		names = append(names, aliasName(in.d.str("subdomain"), in.d.str("domain")))
	}
	for _, a := range in.aliases {
		if a.str("active") == "n" {
			continue
		}
		names = append(names, a.str("domain"))
		if sub := a.str("subdomain"); sub == "www" || sub == "*" {
			names = append(names, aliasName(sub, a.str("domain")))
		}
	}
	if len(names) == 0 {
		vars["alias"] = ""
		return
	}
	// 10 names per directive: the PHP plugin's batch size, kept so the
	// rendered file diffs cleanly against a legacy install.
	var lines []string
	for i := 0; i < len(names); i += 10 {
		lines = append(lines, "ServerAlias "+strings.Join(names[i:min(i+10, len(names))], " "))
	}
	vars["alias"] = strings.Join(lines, "\n\t\t")
}

// aliasName prefixes a domain with its subdomain selector.
func aliasName(sub, domain string) string {
	if sub == "*" {
		return "*." + domain
	}
	return sub + "." + domain
}

// redirectRows builds the redirects loop from redirect_type/redirect_path.
func redirectRows(d row) []map[string]any {
	target := strings.TrimSpace(d.str("redirect_path"))
	rtype := strings.TrimSpace(d.str("redirect_type"))
	if target == "" || rtype == "" || rtype == "no" {
		return nil
	}
	isURL := strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	return []map[string]any{{
		"rewrite_domain":        regexpQuoteHost(d.str("domain")),
		"rewrite_target":        target,
		"rewrite_type":          "[" + rtype + "]",
		"rewrite_is_url":        yn(isURL),
		"rewrite_add_path":      yn(strings.HasSuffix(target, "/")),
		"rewrite_target_is_ssl": yn(strings.HasPrefix(target, "https://")),
	}}
}

// regexpQuoteHost escapes the dots of a host so it is a literal in the
// RewriteCond pattern.
func regexpQuoteHost(h string) string { return strings.ReplaceAll(h, ".", `\.`) }

// buildSEO fills the seo_redirect_* vars from web_domain.seo_redirect.
func buildSEO(in vhostInput, vars map[string]any) {
	mode := strings.TrimSpace(in.d.str("seo_redirect"))
	domain := in.d.str("domain")
	if mode == "" || mode == "non_www_to_www" && domain == "" {
		vars["seo_redirect_enabled"] = false
		return
	}
	origin, target := "", ""
	switch mode {
	case "non_www_to_www":
		origin, target = domain, "www."+domain
	case "www_to_non_www":
		origin, target = "www."+domain, domain
	case "*_to_www_domain_com":
		origin, target = "(.*)", "www."+domain
	case "*_to_domain_com":
		origin, target = "(.*)", domain
	default:
		vars["seo_redirect_enabled"] = false
		return
	}
	vars["seo_redirect_enabled"] = true
	vars["seo_redirect_operator"] = ""
	vars["seo_redirect_origin_domain"] = regexpQuoteHost(origin)
	vars["seo_redirect_target_domain"] = target
}

// renderVhost renders the Apache vhost through the master-template engine
// (custom override dir first).
func renderVhost(in vhostInput, customTplDir string) (string, fpmInfo, error) {
	vars, loops, fpm := buildVhost(in)
	src, _, err := mastertpl.Load(vhostTemplate, customTplDir)
	if err != nil {
		return "", fpm, err
	}
	tpl := mastertpl.New(src)
	for k, v := range vars {
		tpl.SetVar(k, v)
	}
	for k, v := range loops {
		tpl.SetLoop(k, v)
	}
	content, err := tpl.Render()
	if err != nil {
		return "", fpm, fmt.Errorf("apache2: rendering %s: %w", vhostTemplate, err)
	}
	return content, fpm, nil
}
