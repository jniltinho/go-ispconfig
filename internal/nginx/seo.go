package nginx

import "strings"

// rewriteQuote escapes the regex metacharacters ISPConfig escapes in
// rewrite_domain patterns (port of _rewrite_quote).
func rewriteQuote(s string) string {
	r := strings.NewReplacer(".", `\.`, "*", `\*`, "?", `\?`, "+", `\+`)
	return r.Replace(s)
}

// escapeDots escapes only dots (PHP str_replace('.', '\.', ...)).
func escapeDots(s string) string {
	return strings.ReplaceAll(s, ".", `\.`)
}

// seoRedirects ports get_seo_redirects(): the template variables for one
// web_domain's seo_redirect setting, optionally prefixed (alias_...) and
// with the subdomain behavior forced ("none" or "www"; "" keeps the row's
// subdomain setting).
func seoRedirects(web row, prefix, forceSubdomain string) map[string]any {
	out := map[string]any{}
	domain := web.str("domain")
	subdomain := web.str("subdomain")
	if strings.HasPrefix(domain, "*.") {
		subdomain = "*"
	}
	seo := web.str("seo_redirect")

	set := func(origin, target, operator string) {
		out[prefix+"seo_redirect_origin_domain"] = origin
		out[prefix+"seo_redirect_target_domain"] = target
		out[prefix+"seo_redirect_operator"] = operator
	}

	if (subdomain == "www" || subdomain == "*") && forceSubdomain != "www" {
		switch seo {
		case "non_www_to_www":
			set(domain, "www."+domain, "=")
		case "*_domain_tld_to_www_domain_tld":
			set(`^(`+escapeDots(domain)+`|((?:\w+(?:-\w+)*\.)*)((?!www\.)\w+(?:-\w+)*)(\.`+escapeDots(domain)+`))$`,
				"www."+domain, "~*")
		case "*_to_www_domain_tld":
			set("www."+domain, "www."+domain, "!=")
		}
	}
	if forceSubdomain != "none" {
		switch seo {
		case "www_to_non_www":
			set("www."+domain, domain, "=")
		case "*_domain_tld_to_domain_tld":
			set(`^(.+)\.`+escapeDots(domain)+`$`, domain, "~*")
		case "*_to_domain_tld":
			set(domain, domain, "!=")
		}
	}
	return out
}
