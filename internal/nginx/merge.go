package nginx

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// locationLineRe matches a whole location block written on one line so it
// can be expanded into open tag / body / close tag (port of the pattern fed
// to nginx_replace).
var locationLineRe = regexp.MustCompile(
	`^[^\S\n]*location[^\S\n]+(?:(.+)[^\S\n]+)?(.+)[^\S\n]*(\{)[^\S\n]*(##merge##|##delete##)?[^\S\n]*(.+)[^\S\n]*(\})[^\S\n]*(##merge##|##delete##)?[^\S\n]*$`)

// subrootRe finds the ##subroot <path> ## token.
var subrootRe = regexp.MustCompile(`##subroot (.+?)\s*##`)

// safeSubroot validates the ##subroot payload: only [a-z0-9/_.-] characters
// and no ".." (the PHP pattern uses a lookahead RE2 lacks, checked manually
// here).
func safeSubroot(s string) bool {
	if s == "" || strings.Contains(s, "..") {
		return false
	}
	for _, r := range strings.ToLower(s) {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') &&
			r != '/' && r != '_' && r != '-' && r != '.' {
			return false
		}
	}
	return true
}

// expandOneLineLocation rewrites a one-line `location X { ... }` into the
// multi-line form the block parser understands (port of nginx_replace).
func expandOneLineLocation(line string) string {
	m := locationLineRe.FindStringSubmatch(line)
	if m == nil {
		return line
	}
	loc := "location"
	if m[1] != "" {
		loc += " " + m[1]
	}
	loc += " " + m[2] + " " + m[3]
	if m[4] == "##merge##" || m[7] == "##merge##" {
		loc += " ##merge##"
	}
	if m[4] == "##delete##" || m[7] == "##delete##" {
		loc += " ##delete##"
	}
	return loc + "\n" + m[5] + "\n" + m[6]
}

// mergeLocations ports nginx_merge_locations: within the first server block,
// location blocks with the same key are replaced (default), merged
// (##merge##) or deleted (##delete##); comment lines are stripped and the
// ##subroot token appends a sub-path to the first root directive. It returns
// the merged config plus non-fatal warnings.
func mergeLocations(vhostConf string) (string, []error) {
	var warnings []error

	if m := subrootRe.FindStringSubmatch(vhostConf); m != nil {
		if !safeSubroot(m[1]) {
			warnings = append(warnings, fmt.Errorf("nginx: insecure ##subroot token ignored: %q", m[1]))
		} else if rootPos := strings.Index(vhostConf, "root "); rootPos >= 0 {
			if semi := strings.Index(vhostConf[rootPos:], ";"); semi >= 0 {
				insert := rootPos + semi
				vhostConf = vhostConf[:insert] + "/" + strings.TrimPrefix(m[1], "/") + vhostConf[insert:]
			}
		}
	}

	// Pass 1: drop comment lines, rtrim, expand one-line location blocks.
	var expanded []string
	for _, line := range strings.Split(vhostConf, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		expanded = append(expanded, expandOneLineLocation(strings.TrimRight(line, " \t")))
	}
	lines := strings.Split(strings.Join(expanded, "\n"), "\n")

	// Pass 2: collect location blocks of the first server block.
	type block struct {
		action    string
		openTag   string
		body      string
		startLine int
	}
	kept := map[int]string{}
	blocks := map[string]*block{}
	var order []string
	inLocation := false
	level := 0
	serverCount := 0
	var current string

	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "server {") {
			serverCount++
		}
		if serverCount > 1 {
			kept[i] = l
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "location") && !inLocation:
			inLocation = true
			level = 0
			parts := strings.Fields(trimmed)
			var location string
			if len(parts) > 2 && (parts[1] == "=" || parts[1] == "~" || parts[1] == "~*" || parts[1] == "^~") {
				location = parts[1] + " " + parts[2]
			} else if len(parts) > 1 {
				location = parts[1]
			}
			b := blocks[location]
			if b == nil {
				b = &block{action: "replace", openTag: "        location " + location + " {", startLine: i}
				blocks[location] = b
				order = append(order, location)
			}
			if strings.HasSuffix(trimmed, "##merge##") {
				b.action = "merge"
			}
			if strings.HasSuffix(trimmed, "##delete##") {
				b.action = "delete"
			}
			if b.action == "replace" {
				b.body = ""
			}
			current = location
		case inLocation:
			open := strings.LastIndex(l, "{")
			closing := strings.LastIndex(l, "}")
			if open != -1 {
				level++
			}
			switch {
			case closing != -1 && level > 0 && closing >= open:
				level--
				blocks[current].body += l + "\n"
			case closing != -1 && level == 0 && closing >= open:
				inLocation = false
			default:
				blocks[current].body += l + "\n"
			}
		default:
			kept[i] = l
		}
	}

	for _, key := range order {
		b := blocks[key]
		if b.action == "delete" {
			continue
		}
		kept[b.startLine] = b.openTag + "\n" + b.body + "        }"
	}

	idx := make([]int, 0, len(kept))
	for i := range kept {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	out := make([]string, 0, len(idx))
	for _, i := range idx {
		out = append(out, kept[i])
	}
	return strings.TrimSpace(strings.Join(out, "\n")), warnings
}
