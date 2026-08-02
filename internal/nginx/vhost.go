package nginx

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"go-ispconfig/internal/getconf"
)

// vhostInput is everything one vhost render needs, resolved up front (DB
// lookups happen in the handlers; the builder itself is pure and unit
// testable).
type vhostInput struct {
	cfg *getconf.WebConfig
	d   row // the web_domain row being rendered
	// aliases are the active child alias/subdomain rows (types alias and
	// subdomain, not vhost*).
	aliases []row
	// folders are the active web_folder rows of this domain (basic auth).
	folders []row
	// serverPHP is the resolved server_php row for a pinned PHP version,
	// nil for the server default.
	serverPHP row
	// nginxVersion is the running nginx version ("1.26.1").
	nginxVersion string
	// dummyFile is the random "/<hex>.htm" try_files dummy.
	dummyFile string
	// sslFilesExist reports whether the site's crt+key files exist non-empty.
	sslFilesExist bool
	// urlIsLocal reports whether a redirect target host is served by this
	// server (port of url_is_local, DB-backed in the handler). nil falls
	// back to a plain host comparison.
	urlIsLocal func(host string) bool
}

// fpmInfo is the PHP-FPM addressing derived for a domain: consumed by the
// vhost template, the {FASTCGIPASS} placeholder and the pool tasks.
type fpmInfo struct {
	poolName   string
	poolDir    string
	socketDir  string
	socketPath string
	port       int64
	useSocket  bool
	initScript string
}

// resolveFPM ports the pool/socket/port derivation of update(): server_php
// overrides the server defaults, the pool is named web<domain_id>, the TCP
// port is php_fpm_start_port + domain_id - 1.
func resolveFPM(cfg *getconf.WebConfig, d row, serverPHP row) fpmInfo {
	poolDir := cfg.PHPFPMPoolDir
	socketDir := cfg.PHPFPMSocketDir
	initScript := cfg.PHPFPMInitScript
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

// atoi64 parses a config number stored as string (0 when empty/invalid).
func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// versionGE compares dot-separated numeric versions.
func versionGE(v, min string) bool {
	if v == "" {
		return false
	}
	va, vb := strings.Split(v, "."), strings.Split(min, ".")
	for i := 0; i < len(va) || i < len(vb); i++ {
		var a, b int
		if i < len(va) {
			a, _ = strconv.Atoi(va[i]) // non-numeric parts read as 0
		}
		if i < len(vb) {
			b, _ = strconv.Atoi(vb[i])
		}
		if a != b {
			return a > b
		}
	}
	return true
}

// buildVhost assembles the template variables and loops for
// nginx_vhost.conf.master (port of the update() vector building) and the
// derived FPM addressing.
func buildVhost(in vhostInput) (map[string]any, map[string][]map[string]any, fpmInfo) {
	d, cfg := in.d, in.cfg
	vars := map[string]any{}
	loops := map[string][]map[string]any{}
	fpm := resolveFPM(cfg, d, in.serverPHP)

	webFolder := webFolderOf(d)
	docroot := d.str("document_root")
	domain := d.str("domain")

	// Copy the row fields the template reads directly.
	for _, k := range []string{
		"domain", "document_root", "system_user", "ssi", "cgi",
		"disable_symlinknotowner", "rewrite_to_https", "php_fpm_chroot",
	} {
		vars[k] = d.str(k)
	}
	vars["errordocs"] = d.num("errordocs")

	ip := d.str("ip_address")
	if ip == "" {
		ip = "*"
	}
	vars["ip_address"] = ip
	httpPort, httpsPort := d.num("http_port"), d.num("https_port")
	if httpPort == 0 {
		httpPort = 80
	}
	if httpsPort == 0 {
		httpsPort = 443
	}
	vars["http_port"] = fmt.Sprint(httpPort)
	vars["https_port"] = fmt.Sprint(httpsPort)
	if v6 := d.str("ipv6_address"); v6 != "" {
		vars["ipv6_enabled"] = 1
		vars["ipv6_address"] = v6
	}
	if ip == "*" && d.str("ipv6_address") == "" {
		vars["ipv6_wildcard"] = 1
	}

	vars["web_document_root"] = docroot + "/" + webFolder
	vars["web_document_root_www"] = cfg.WebsiteBasedir + "/" + domain + "/" + webFolder
	vars["web_basedir"] = cfg.WebsiteBasedir

	// PHP mode: 'fast-cgi' rows are legacy php-fpm.
	php := d.str("php")
	if php == "fast-cgi" {
		php = "php-fpm"
	}
	vars["php"] = php
	if fpm.useSocket {
		vars["use_tcp"], vars["use_socket"] = 0, 1
	} else {
		vars["use_tcp"], vars["use_socket"] = 1, 0
	}
	vars["fpm_socket"] = fpm.socketPath
	vars["fpm_port"] = fpm.port
	vars["php_fpm_chroot_web_folder"] = "/" + strings.Trim(webFolder, "/")
	vars["rnd_php_dummy_file"] = in.dummyFile

	// SSL: enabled only when the cert files actually exist.
	sslDomain := d.str("ssl_domain")
	if sslDomain == "" {
		sslDomain = domain
	}
	vars["ssl_domain"] = sslDomain
	vars["ssl_crt_file"] = docroot + "/ssl/" + sslDomain + ".crt"
	vars["ssl_key_file"] = docroot + "/ssl/" + sslDomain + ".key"
	vars["ssl_bundle_file"] = docroot + "/ssl/" + sslDomain + ".bundle"
	if d.str("ssl") == "y" && in.sslFilesExist {
		vars["ssl_enabled"] = 1
	} else {
		vars["ssl_enabled"] = 0
	}

	// TLS 1.3 / http2 quirk by nginx version.
	// ponytail: PHP additionally probes the OpenSSL build/runtime version;
	// every supported distro (Debian 11+, Ubuntu 22.04+) links >= 1.1.1, so
	// nginx >= 1.13.0 alone decides.
	if vars["ssl_enabled"] == 1 && versionGE(in.nginxVersion, "1.13.0") {
		vars["tls13_supported"] = "y"
	}
	if !versionGE(in.nginxVersion, "1.25.1") {
		vars["http2_directive_compat_quirk"] = " http2"
	}
	vars["nginx_version"] = in.nginxVersion
	vars["nginx_full_version"] = in.nginxVersion

	// Proxy protocol settings from the server config.
	protocols := strings.Split(cfg.VhostProxyProtocolProtocols, ",")
	siteOn := cfg.VhostProxyProtocolEnabled == "all" ||
		(cfg.VhostProxyProtocolEnabled == "y" && d.str("proxy_protocol") == "y")
	vars["proxy_protocol_http"] = atoi64(cfg.VhostProxyProtocolHTTPPort)
	vars["proxy_protocol_https"] = atoi64(cfg.VhostProxyProtocolHTTPSPort)
	vars["use_proxy_protocol"] = yn(siteOn && contains(protocols, "ipv4"))
	vars["use_proxy_protocol_ipv6"] = yn(siteOn && contains(protocols, "ipv6"))

	logging := cfg.Logging
	if logging == "" {
		logging = "yes"
	}
	vars["logging"] = logging
	vars["enable_pagespeed"] = "n" // stats/pagespeed are out of scope

	// SEO redirect of the domain itself.
	vars["seo_redirect_enabled"] = 0
	if d.str("seo_redirect") != "" {
		if seo := seoRedirects(d, "", ""); len(seo) > 0 {
			vars["seo_redirect_enabled"] = 1
			for k, v := range seo {
				vars[k] = v
			}
		}
	}

	// Own redirect (Redirect tab of the domain itself).
	buildOwnRedirect(in, vars, loops)

	// Aliases: server_name additions, SEO redirects, local and external
	// rewrites.
	buildAliases(in, vars, loops)

	// Custom rewrite rules (validated all-or-nothing).
	loops["rewrite_rules"] = validRewriteRules(d.str("rewrite_rules"))

	// Stats folder auth file (the stats engines themselves are later work,
	// but the template always references the path).
	vars["stats_auth_passwd_file"] = docroot + "/" + webFolder + "/stats/.htpasswd_stats"

	// Basic auth locations from the protected folders.
	var authLocs []map[string]any
	for _, f := range in.folders {
		p := trimSlashes(f.str("path"))
		if p != "" {
			p += "/"
		}
		authLocs = append(authLocs, map[string]any{
			"htpasswd_location": "/" + p,
			"htpasswd_path":     docroot + "/" + webFolder + "/" + p,
		})
	}
	loops["basic_auth_locations"] = authLocs

	return vars, loops, fpm
}

// yn maps a bool to the 'y'/'n' strings the template compares against.
func yn(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if strings.TrimSpace(e) == s {
			return true
		}
	}
	return false
}

// proxyDirectiveRows splits a proxy_directives text into template loop rows
// (nil for none — PHP uses false so the tmpl_if stays falsy).
func proxyDirectiveRows(text string) []map[string]any {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	// Proxy directives are the same class of user-supplied config as
	// nginx_directives, so they pass the same blacklist (dropping e.g.
	// load_module). They run in a location block, not the server block.
	filtered, _ := filterBlacklistedDirectives(text)
	filtered = strings.ReplaceAll(filtered, "\r\n", "\n")
	filtered = strings.ReplaceAll(filtered, "\r", "\n")
	var rows []map[string]any
	for _, line := range strings.Split(filtered, "\n") {
		rows = append(rows, map[string]any{"proxy_directive": line})
	}
	return rows
}

// proxyRows adapts proxyDirectiveRows to the any-typed loop cell (false for
// none, like PHP).
func proxyRows(text string) any {
	if rows := proxyDirectiveRows(text); rows != nil {
		return rows
	}
	return false
}

// rewriteExclude builds the (?!/(path|stats|error|acme))/ negative lookahead
// for a relative redirect path.
func rewriteExclude(relPath string, errordocs bool) string {
	inner := strings.TrimSuffix(strings.TrimPrefix(relPath, "/"), "/")
	if inner != "" {
		inner += "|"
	}
	inner += "stats"
	if errordocs {
		inner += "|error"
	}
	return `(?!/(` + inner + `|\.well-known/acme-challenge))/`
}

// parseRedirectURL parses a redirect_path URL, mapping a $scheme prefix to
// http first (PHP parse_url usage).
func parseRedirectURL(raw string) *url.URL {
	if strings.HasPrefix(raw, "$scheme") {
		raw = "http" + strings.TrimPrefix(raw, "$scheme")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return u
}

// normalizedURLPath returns the URL path without trailing slash and with a
// leading slash.
func normalizedURLPath(u *url.URL) string {
	p := strings.TrimSuffix(u.Path, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// defaultPort reports whether the URL uses the default web ports.
func defaultPort(u *url.URL) bool {
	return u.Port() == "" || u.Port() == "80" || u.Port() == "443"
}

// buildOwnRedirect ports the redirect_type/redirect_path switch of update():
// it fills the own_redirects loop and adjusts the document-root vars for
// local proxy targets.
func buildOwnRedirect(in vhostInput, vars map[string]any, loops map[string][]map[string]any) {
	d := in.d
	redirectType, redirectPath := d.str("redirect_type"), d.str("redirect_path")
	if redirectType == "" || redirectPath == "" {
		return
	}
	isProxy := redirectType == "proxy"
	if strings.HasPrefix(redirectPath, "[scheme]") {
		scheme := "$scheme"
		if isProxy {
			scheme = "http"
		}
		redirectPath = scheme + strings.TrimPrefix(redirectPath, "[scheme]")
	}
	proxyDirs := any(false)
	if isProxy {
		proxyDirs = proxyRows(d.str("proxy_directives"))
	}

	domain := d.str("domain")
	subdomain := d.str("subdomain")
	errordocs := d.num("errordocs") == 1

	var rewriteDomain string
	switch subdomain {
	case "*":
		rewriteDomain = `(^|\.)` + rewriteQuote(domain)
	default: // www and none share the anchored form
		rewriteDomain = "^" + rewriteQuote(domain)
	}

	localHost := func(host string) bool {
		switch subdomain {
		case "www":
			return host == domain || host == "www."+domain
		case "*":
			if in.urlIsLocal != nil {
				return in.urlIsLocal(host)
			}
			return host == domain || strings.HasSuffix(host, "."+domain)
		default:
			return host == domain
		}
	}

	rewriteExcl, rewriteSubdir, excludeOwnHostname := "", "", ""
	if strings.HasPrefix(redirectPath, "/") { // relative path
		if isProxy {
			vars["web_document_root_www_proxy"] = "root " + vars["web_document_root_www"].(string) + ";"
			vars["web_document_root_www"] = vars["web_document_root_www"].(string) + strings.TrimSuffix(redirectPath, "/")
			return
		}
		rewriteExcl = rewriteExclude(redirectPath, errordocs)
	} else { // URL target
		u := parseRedirectURL(redirectPath)
		if localHost(u.Hostname()) && defaultPort(u) {
			p := normalizedURLPath(u)
			if isProxy {
				vars["web_document_root_www_proxy"] = "root " + vars["web_document_root_www"].(string) + ";"
				vars["web_document_root_www"] = vars["web_document_root_www"].(string) + p
				return
			}
			rewriteExcl = rewriteExclude(p+"/", errordocs)
			excludeOwnHostname = u.Hostname()
		} else {
			rewriteExcl = `(?!/(\.well-known/acme-challenge))/`
			if isProxy {
				vars["use_proxy"] = "y"
				rewriteSubdir = strings.TrimPrefix(u.Path, "/")
				// The www and * cases append a trailing slash; the plain
				// subdomain case keeps it as-is (PHP parity).
				if subdomain == "www" || subdomain == "*" {
					if rewriteSubdir != "" && !strings.HasSuffix(rewriteSubdir, "/") {
						rewriteSubdir += "/"
					}
				}
				if rewriteSubdir == "/" {
					rewriteSubdir = ""
				}
			}
		}
	}

	if redirectType == "no" {
		redirectType = ""
	}
	loops["own_redirects"] = append(loops["own_redirects"], map[string]any{
		"rewrite_domain":       rewriteDomain,
		"rewrite_type":         redirectType,
		"rewrite_target":       redirectPath,
		"rewrite_exclude":      rewriteExcl,
		"rewrite_subdir":       rewriteSubdir,
		"exclude_own_hostname": excludeOwnHostname,
		"proxy_directives":     proxyDirs,
		"use_rewrite":          !isProxy,
		"use_proxy":            isProxy,
	})
}

// buildAliases ports the alias/subdomain loop of update(): server_name
// additions, alias SEO redirects, local rewrites inside the vhost and
// external redirect server blocks.
func buildAliases(in vhostInput, vars map[string]any, loops map[string][]map[string]any) {
	d := in.d
	domain := d.str("domain")
	errordocs := d.num("errordocs") == 1

	var serverAlias []string
	switch d.str("subdomain") {
	case "www":
		serverAlias = append(serverAlias, "www."+domain+" ")
	case "*":
		serverAlias = append(serverAlias, "*."+domain+" ")
	}

	seoOwn := d.str("seo_redirect")
	for _, alias := range in.aliases {
		aliasProxyDirs := any(false)
		if alias.str("redirect_type") == "proxy" {
			aliasProxyDirs = proxyRows(alias.str("proxy_directives"))
		}
		redirectType, redirectPath := alias.str("redirect_type"), alias.str("redirect_path")

		if redirectType == "" || redirectPath == "" || strings.HasPrefix(redirectPath, "/") {
			switch alias.str("subdomain") {
			case "www":
				serverAlias = append(serverAlias, "www."+alias.str("domain")+" "+alias.str("domain")+" ")
			case "*":
				serverAlias = append(serverAlias, "*."+alias.str("domain")+" "+alias.str("domain")+" ")
			default:
				serverAlias = append(serverAlias, alias.str("domain")+" ")
			}

			// SEO redirects for alias domains inside the main server block.
			if alias.str("seo_redirect") != "" && seoOwn != "*_to_www_domain_tld" && seoOwn != "*_to_domain_tld" &&
				(alias.str("type") == "alias" ||
					(alias.str("type") == "subdomain" && seoOwn != "*_domain_tld_to_www_domain_tld" && seoOwn != "*_domain_tld_to_domain_tld")) {
				if seo := seoRedirects(alias, "alias_", ""); len(seo) > 0 {
					loops["alias_seo_redirects"] = append(loops["alias_seo_redirects"], seo)
				}
			}
		}

		// Local rewriting inside the vhost server block.
		if redirectType != "" && strings.HasPrefix(redirectPath, "/") && redirectType != "proxy" {
			if !strings.HasSuffix(redirectPath, "/") {
				redirectPath += "/"
			}
			excl := rewriteExclude(redirectPath, errordocs)
			localType := redirectType
			if localType == "no" {
				localType = ""
			}
			add := func(origin, operator string) {
				loops["local_redirects"] = append(loops["local_redirects"], map[string]any{
					"local_redirect_origin_domain": origin,
					"local_redirect_operator":      operator,
					"local_redirect_exclude":       excl,
					"local_redirect_target":        redirectPath,
					"local_redirect_type":          localType,
				})
			}
			switch alias.str("subdomain") {
			case "www":
				add(alias.str("domain"), "=")
				add("www."+alias.str("domain"), "=")
			case "*":
				add(`^(`+escapeDots(alias.str("domain"))+`|.+\.`+escapeDots(alias.str("domain"))+`)$`, "~*")
			default:
				add(alias.str("domain"), "=")
			}
		}

		// External rewriting: dedicated server blocks.
		if redirectType != "" && redirectPath != "" && !strings.HasPrefix(redirectPath, "/") {
			if !strings.HasSuffix(redirectPath, "/") {
				redirectPath += "/"
			}
			if strings.HasPrefix(redirectPath, "[scheme]") {
				scheme := "$scheme"
				if redirectType == "proxy" {
					scheme = "http"
				}
				redirectPath = scheme + strings.TrimPrefix(redirectPath, "[scheme]")
			}
			isProxy := redirectType == "proxy"
			rewriteSubdir := ""
			if isProxy {
				u := parseRedirectURL(redirectPath)
				rewriteSubdir = strings.TrimPrefix(u.Path, "/")
				if rewriteSubdir != "" && !strings.HasSuffix(rewriteSubdir, "/") {
					rewriteSubdir += "/"
				}
				if rewriteSubdir == "/" {
					rewriteSubdir = ""
				}
			} else {
				redirectPath = strings.TrimSuffix(redirectPath, "/")
			}
			extType := redirectType
			if extType == "no" {
				extType = ""
			}

			addExternal := func(rewriteDomain, forceSubdomain string) {
				var seoRows any = false
				if alias.str("seo_redirect") != "" {
					if seo := seoRedirects(alias, "alias_", forceSubdomain); len(seo) > 0 {
						seoRows = []map[string]any{seo}
					}
				}
				loops["redirects"] = append(loops["redirects"], map[string]any{
					"rewrite_domain":       rewriteDomain,
					"rewrite_type":         extType,
					"rewrite_target":       redirectPath,
					"rewrite_subdir":       rewriteSubdir,
					"proxy_directives":     aliasProxyDirs,
					"use_rewrite":          !isProxy,
					"use_proxy":            isProxy,
					"alias_seo_redirects2": seoRows,
				})
			}
			switch alias.str("subdomain") {
			case "www":
				addExternal(alias.str("domain"), "none")
				addExternal("www."+alias.str("domain"), "www")
			case "*":
				addExternal(alias.str("domain")+" *."+alias.str("domain"), "")
			default:
				domainRule := alias.str("domain")
				forceSubdomain := "none"
				if strings.HasPrefix(domainRule, "*.") {
					forceSubdomain = ""
				}
				addExternal(domainRule, forceSubdomain)
			}
		}
	}

	vars["alias"] = strings.TrimSpace(strings.Join(serverAlias, ""))
}
