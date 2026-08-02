package installer

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// nowUnix is the backup-suffix clock, overridable in tests.
var nowUnix = func() int64 { return time.Now().Unix() }

// writeFileBackup writes content to path with mode (parent dirs created),
// implementing the design D6 safety rule: an existing file with different
// content is first copied to <path>.bak-<unix-ts>; identical content is a
// no-op (changed=false, no backup churn). The returned restore func undoes
// the write (recreates the previous content, or removes a file that did
// not exist) so config steps can roll back when nginx -t/named-checkconf
// fails; it is non-nil only when changed.
func writeFileBackup(path string, content []byte, mode os.FileMode) (changed bool, restore func() error, err error) {
	old, readErr := os.ReadFile(path)
	exists := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, nil, fmt.Errorf("reading %s: %w", path, readErr)
	}
	if exists && bytes.Equal(old, content) {
		return false, nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, nil, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if exists {
		backup := fmt.Sprintf("%s.bak-%d", path, nowUnix())
		if err := os.WriteFile(backup, old, 0o600); err != nil {
			return false, nil, fmt.Errorf("backing up %s: %w", path, err)
		}
		restore = func() error { return os.WriteFile(path, old, mode) }
	} else {
		restore = func() error { return os.Remove(path) }
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		return false, nil, fmt.Errorf("writing %s: %w", path, err)
	}
	// Re-assert mode: os.WriteFile does not chmod an existing file.
	if err := os.Chmod(path, mode); err != nil {
		return false, nil, fmt.Errorf("chmod %s: %w", path, err)
	}
	return true, restore, nil
}
