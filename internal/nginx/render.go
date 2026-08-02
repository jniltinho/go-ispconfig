package nginx

import (
	"fmt"
	"strings"

	"go-ispconfig/internal/mastertpl"
)

// vhostTemplate is the master template name for nginx vhosts.
const vhostTemplate = "nginx_vhost.conf.master"

// directiveRows turns the custom nginx_directives field into template loop
// rows: blacklist filter (strip + report), master-template pass over the
// directives themselves (they may contain <tmpl_if> logic, PHP parity) and
// {DOCROOT}/{DOCROOT_CLIENT}/{DOMAIN}/{FASTCGIPASS} substitution. The
// returned warnings are non-fatal (the vhost renders without the offending
// lines) and end up as datalog errors.
func directiveRows(d row, vars map[string]any, fpm fpmInfo) ([]map[string]any, []error) {
	directives := d.str("nginx_directives")
	if strings.TrimSpace(directives) == "" {
		return nil, nil
	}

	// Directives may use template logic (use_socket, fpm_socket, ...). Render
	// FIRST, then blacklist-filter the rendered text: a forbidden directive
	// hidden inside a <tmpl_if> would not match the raw-line regex but would
	// reappear after rendering, so filtering the render output is the only
	// place the blacklist actually holds.
	tpl := mastertpl.New(directives)
	for k, v := range vars {
		tpl.SetVar(k, v)
	}
	var warnings []error
	if rendered, err := tpl.Render(); err == nil {
		directives = rendered // an empty render is a valid result (drops the block)
	} else {
		warnings = append(warnings, fmt.Errorf("nginx: custom directives template error (kept verbatim): %w", err))
	}

	filtered, rejected := filterBlacklistedDirectives(directives)
	directives = filtered
	warnings = append(warnings, rejected...)
	if strings.TrimSpace(directives) == "" {
		return nil, warnings
	}

	fastcgiPass := "fastcgi_pass 127.0.0.1:" + fmt.Sprint(fpm.port) + ";"
	if fpm.useSocket {
		fastcgiPass = "fastcgi_pass unix:" + fpm.socketPath + ";"
	}
	trans := strings.NewReplacer(
		"{DOCROOT}", row(vars).str("web_document_root_www"),
		"{DOCROOT_CLIENT}", row(vars).str("web_document_root"),
		"{DOMAIN}", row(vars).str("domain"),
		"{FASTCGIPASS}", fastcgiPass,
	)
	directives = strings.ReplaceAll(directives, "\r\n", "\n")
	directives = strings.ReplaceAll(directives, "\r", "\n")
	var rows []map[string]any
	for _, line := range strings.Split(directives, "\n") {
		rows = append(rows, map[string]any{"nginx_directive": trans.Replace(line)})
	}
	return rows, warnings
}

// renderVhost builds the template vector and renders the vhost through the
// master-template renderer (custom override dir first). The returned
// warnings are non-fatal rejections (blacklisted directives); err is fatal.
func renderVhost(in vhostInput, customTplDir string) (content string, warnings []error, err error) {
	vars, loops, fpm := buildVhost(in)
	loops["nginx_directives"], warnings = directiveRows(in.d, vars, fpm)

	src, _, err := mastertpl.Load(vhostTemplate, customTplDir)
	if err != nil {
		return "", warnings, err
	}
	tpl := mastertpl.New(src)
	for k, v := range vars {
		tpl.SetVar(k, v)
	}
	for k, v := range loops {
		tpl.SetLoop(k, v)
	}
	content, err = tpl.Render()
	if err != nil {
		return "", warnings, fmt.Errorf("nginx: rendering %s: %w", vhostTemplate, err)
	}
	return content, warnings, nil
}
