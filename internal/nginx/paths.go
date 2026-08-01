package nginx

import (
	"fmt"
	"path/filepath"
	"strings"
)

// safeSitePath validates that path is an absolute, traversal-free path
// strictly inside basedir (never basedir itself, never /). Every
// creating or destructive filesystem operation of the plugin goes through
// this check (design D4: never touch a path outside website_basedir).
func safeSitePath(path, basedir string) error {
	if basedir == "" || !filepath.IsAbs(basedir) {
		return fmt.Errorf("nginx: website_basedir %q is not an absolute path", basedir)
	}
	if path == "" || !filepath.IsAbs(path) {
		return fmt.Errorf("nginx: path %q is not absolute", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("nginx: path %q contains a parent-directory traversal", path)
	}
	clean := filepath.Clean(path)
	base := filepath.Clean(basedir)
	if clean == "/" || clean == base {
		return fmt.Errorf("nginx: refusing to operate on %q", clean)
	}
	if !strings.HasPrefix(clean, base+string(filepath.Separator)) {
		return fmt.Errorf("nginx: path %q is outside website_basedir %q", clean, base)
	}
	return nil
}
