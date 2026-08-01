package nginx

import (
	"regexp"
	"strings"
)

// rewriteRuleRes is the whitelist of custom rewrite_rules line shapes (port
// of the update() validation block): rewrite/if/break/return/set lines and
// closing braces. A line matching none of these invalidates the whole block.
var rewriteRuleRes = []*regexp.Regexp{
	// rewrite <pattern> <target> [flag];
	regexp.MustCompile(`^\s*rewrite\s+(^/)?\S+(\$)?\s+\S+(\s+(last|break|redirect|permanent|))?\s*;\s*$`),
	regexp.MustCompile(`^\s*rewrite\s+(^/)?('[^']+'|"[^"]+")+(\$)?\s+('[^']+'|"[^"]+")+(\s+(last|break|redirect|permanent|))?\s*;\s*$`),
	regexp.MustCompile(`^\s*rewrite\s+(^/)?('[^']+'|"[^"]+")+(\$)?\s+\S+(\s+(last|break|redirect|permanent|))?\s*;\s*$`),
	regexp.MustCompile(`^\s*rewrite\s+(^/)?\S+(\$)?\s+('[^']+'|"[^"]+")+(\s+(last|break|redirect|permanent|))?\s*;\s*$`),
	// break;
	regexp.MustCompile(`^\s*break\s*;\s*$`),
	// return <code> [text];
	regexp.MustCompile(`^\s*return\s+\d\d\d.*;\s*$`),
	// return [code] URL;
	regexp.MustCompile(`^\s*return(\s+\d\d\d)?\s+(http|https|ftp)://([a-zA-Z0-9.\-]+(:[a-zA-Z0-9.&%$\-]+)*@)*((25[0-5]|2[0-4][0-9]|[0-1]{1}[0-9]{2}|[1-9]{1}[0-9]{1}|[1-9])\.(25[0-5]|2[0-4][0-9]|[0-1]{1}[0-9]{2}|[1-9]{1}[0-9]{1}|[1-9]|0)\.(25[0-5]|2[0-4][0-9]|[0-1]{1}[0-9]{2}|[1-9]{1}[0-9]{1}|[1-9]|0)\.(25[0-5]|2[0-4][0-9]|[0-1]{1}[0-9]{2}|[1-9]{1}[0-9]{1}|[0-9])|localhost|([a-zA-Z0-9\-]+\.)*[a-zA-Z0-9\-]+\.(com|edu|gov|int|mil|net|org|biz|arpa|info|name|pro|aero|coop|museum|[a-zA-Z]{2}))(:[0-9]+)*(/($|[a-zA-Z0-9.,?'\\+&%$#=~_\-]+))*\s*;\s*$`),
	// set $var value;
	regexp.MustCompile(`^\s*set\s+\$\S+\s+\S+\s*;\s*$`),
}

// ifOpenRes match the two allowed `if (...) {` shapes; they additionally
// track brace nesting.
var ifOpenRes = []*regexp.Regexp{
	regexp.MustCompile(`^\s*if\s+\(\s*\$\S+(\s+(!?(=|~|~\*))\s+(\S+|".+"))?\s*\)\s*\{\s*$`),
	regexp.MustCompile(`^\s*if\s+\(\s*!?-(f|d|e|x)\s+\S+\s*\)\s*\{\s*$`),
}

// validRewriteRules ports the custom rewrite_rules whitelist: it returns the
// accepted lines as template loop rows, or nil when any line is invalid or
// the braces are unbalanced (PHP behavior: all-or-nothing).
func validRewriteRules(rules string) []map[string]any {
	rules = strings.TrimSpace(rules)
	if rules == "" {
		return nil
	}
	rules = strings.ReplaceAll(rules, "\r\n", "\n")
	rules = strings.ReplaceAll(rules, "\r", "\n")

	var out []map[string]any
	ifLevel := 0
lines:
	for _, line := range strings.Split(rules, "\n") {
		add := func() { out = append(out, map[string]any{"rewrite_rule": line}) }
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			add()
			continue
		}
		if trimmed == "}" {
			ifLevel--
			add()
			continue
		}
		for _, re := range ifOpenRes {
			if re.MatchString(line) {
				ifLevel++
				add()
				continue lines
			}
		}
		for _, re := range rewriteRuleRes {
			if re.MatchString(line) {
				add()
				continue lines
			}
		}
		return nil // invalid line: drop the whole block
	}
	if ifLevel != 0 {
		return nil
	}
	return out
}
