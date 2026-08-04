package apitoken

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Scope actions. `write` implies `read`: a credential that may change a
// resource can obviously see it, and forcing both strings onto every token
// would only produce tokens that are wrong in one direction.
const (
	ActionRead  = "read"
	ActionWrite = "write"
	Wildcard    = "*"
)

// Resources are the API's top-level groups. A scope names one of these (or
// `*`), never an individual endpoint: the panel's surface is
// resource-oriented, so a new endpoint inherits its group's resource instead
// of needing a grant name that every existing token silently lacks.
var Resources = []string{
	"clients", "sites", "mail", "dns", "monitor", "server", "system",
}

// ActionFor maps an HTTP method onto the action it requires.
func ActionFor(method string) string {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		return ActionRead
	}
	return ActionWrite
}

// ValidateScopes checks a scope list against the grammar and reports the
// first offending entry. An empty list is refused: a token must grant
// something explicitly, never by omission.
func ValidateScopes(scopes []string) error {
	if len(scopes) == 0 {
		return fmt.Errorf("apitoken: at least one scope is required")
	}
	known := map[string]bool{Wildcard: true}
	for _, r := range Resources {
		known[r] = true
	}
	for _, s := range scopes {
		resource, action, ok := strings.Cut(s, ":")
		if !ok || !known[resource] {
			return fmt.Errorf("apitoken: unknown scope %q", s)
		}
		if action != ActionRead && action != ActionWrite && action != Wildcard {
			return fmt.Errorf("apitoken: unknown scope %q", s)
		}
	}
	return nil
}

// Allows reports whether a scope list covers a resource and action.
func Allows(scopes []string, resource, action string) bool {
	for _, s := range scopes {
		grantedResource, grantedAction, ok := strings.Cut(s, ":")
		if !ok {
			continue
		}
		if grantedResource != Wildcard && grantedResource != resource {
			continue
		}
		switch grantedAction {
		case Wildcard, action:
			return true
		case ActionWrite:
			// write implies read, never the other way round.
			if action == ActionRead {
				return true
			}
		}
	}
	return false
}

// AllScopes is the full grant, used by the CLI's convenience flag and by the
// UI's "everything" preset. It is still bounded by the owner's own rights.
func AllScopes() []string {
	out := make([]string, 0, len(Resources))
	for _, r := range Resources {
		out = append(out, r+":"+Wildcard)
	}
	sort.Strings(out)
	return out
}

// ResourceForPath maps a request path under /api to its resource group. It is
// the single place route grouping is expressed, so an endpoint added to an
// existing group is scoped without touching this file.
//
// Paths that carry no resource of their own (login, logout, session, the form
// metadata and lookups) return "", meaning "no scope required" — they are
// either unauthenticated or pure descriptions of the surface the caller may
// already reach.
func ResourceForPath(path string) string {
	p := strings.TrimPrefix(path, "/api")
	p = strings.TrimPrefix(p, "/")
	// Exchanging a token for a JWT grants nothing new — the JWT carries the
	// issuing token's own scopes — so requiring system:write for it would
	// mean only broadly-scoped tokens could use the narrow credential.
	if p == "tokens/exchange" {
		return ""
	}
	head, _, _ := strings.Cut(p, "/")
	switch head {
	case "clients", "resellers", "client-templates", "client-message-templates":
		return "clients"
	case "sites":
		return "sites"
	case "mail":
		return "mail"
	case "dns":
		return "dns"
	case "monitor":
		return "monitor"
	case "server", "servers", "server_ip", "firewall", "fail2ban":
		return "server"
	case "system", "cp-users", "tokens":
		return "system"
	default:
		return ""
	}
}
