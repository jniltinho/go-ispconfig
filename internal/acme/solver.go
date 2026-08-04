package acme

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/challenge/http01"
)

// Webroot is the one directory every vhost serves /.well-known/acme-challenge
// from. It is the legacy's path (letsencrypt.inc.php passes it as
// --webroot-path) and both vhost templates hardcode it, so the solver writes
// where nginx and Apache already look.
const Webroot = "/usr/local/ispconfig/interface/acme"

// webrootSolver answers http-01 by dropping the token under the shared
// webroot. It binds no listener: the web server owns :80, and a solver that
// wanted the port would have to stop it.
type webrootSolver struct{ root string }

// NewWebrootSolver returns the http-01 provider rooted at root (Webroot when
// empty).
func NewWebrootSolver(root string) *webrootSolver { //nolint:revive // returned into lego's interface
	if root == "" {
		root = Webroot
	}
	return &webrootSolver{root: root}
}

// challengePath is where lego expects the token to be reachable from, mapped
// onto the filesystem under the webroot.
func (s *webrootSolver) challengePath(token string) string {
	return filepath.Join(s.root, filepath.FromSlash(http01.ChallengePath(token)))
}

// Present writes the key authorization where the vhost will serve it.
func (s *webrootSolver) Present(_, token, keyAuth string) error {
	path := s.challengePath(token)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("acme: creating challenge dir: %w", err)
	}
	// 0644: the web server runs as another user and has to read it.
	if err := writeFileAtomic(path, []byte(keyAuth), 0o644); err != nil {
		return fmt.Errorf("acme: writing challenge: %w", err)
	}
	return nil
}

// CleanUp removes the token. A leftover is harmless but it is one more file
// served from a public path than the panel needs.
func (s *webrootSolver) CleanUp(_, token, _ string) error {
	if err := os.Remove(s.challengePath(token)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("acme: removing challenge: %w", err)
	}
	return nil
}
