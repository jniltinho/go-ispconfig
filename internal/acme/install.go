package acme

import (
	"fmt"
	"os"
	"time"
)

// LinkSiteCerts symlinks the certbot live paths into a site's ssl directory
// (<docroot>/ssl/<domain>-le.{key,crt}), matching the legacy install-cert
// layout both web plugins already read.
func LinkSiteCerts(fullchain, privkey, keyFile, crtFile string) error {
	for _, f := range []string{keyFile, crtFile} {
		if err := removeLinkOnly(f); err != nil {
			return err
		}
	}
	if err := linkFile(keyFile, privkey); err != nil {
		return err
	}
	return linkFile(crtFile, fullchain)
}

// linkFile symlinks target → source, replacing a stale link and backing up a
// real file (port of link_file in letsencrypt.inc.php).
func linkFile(target, source string) error {
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if existing, _ := os.Readlink(target); existing == source {
				return nil
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("acme: replacing link %s: %w", target, err)
			}
		} else {
			backup := target + ".old." + time.Now().Format("20060102150405")
			if err := copyFileMode(target, backup, 0o400); err != nil {
				return fmt.Errorf("acme: backing up %s: %w", target, err)
			}
			if err := os.Remove(target); err != nil {
				return fmt.Errorf("acme: removing %s: %w", target, err)
			}
		}
	}
	if err := os.Symlink(source, target); err != nil {
		return fmt.Errorf("acme: linking %s -> %s: %w", target, source, err)
	}
	return nil
}

func removeLinkOnly(target string) error {
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(target)
	}
	return nil
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}
