package dns

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// versionRe extracts the numeric version from `named -v` output like
// "BIND 9.18.28-0ubuntu0.24.04.1-Ubuntu (Extended Support Version)".
var versionRe = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// caaMinVersion is the first BIND release with native CAA support.
var caaMinVersion = [3]int{9, 9, 6}

// bindCAASupported probes `named -v` once per daemon run (design D3) and
// reports whether the installed BIND understands CAA records natively
// (>= 9.9.6). A missing or unparsable named answers false, like the PHP
// is_executable guard.
func (p *Plugin) bindCAASupported(ctx context.Context) bool {
	p.caaOnce.Do(func() {
		if p.caaProbed != nil { // test preset
			p.caaSupported = *p.caaProbed
			return
		}
		out, err := p.runner.Run(ctx, "named", "-v")
		if err != nil {
			p.log.Warn("dns: named -v probe failed, assuming no CAA support", "error", err)
			return
		}
		p.caaSupported = caaSupportedByVersion(string(out))
		p.log.Info("dns: probed bind version", "output", firstLine(string(out)), "caa_supported", p.caaSupported)
	})
	return p.caaSupported
}

// caaSupportedByVersion parses the first version number in the `named -v`
// output and compares it against 9.9.6.
func caaSupportedByVersion(out string) bool {
	m := versionRe.FindStringSubmatch(firstLine(out))
	if m == nil {
		return false
	}
	var v [3]int
	for i := 0; i < 3; i++ {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	for i := 0; i < 3; i++ {
		if v[i] != caaMinVersion[i] {
			return v[i] > caaMinVersion[i]
		}
	}
	return true
}

// firstLine returns the first line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
