package installer

import (
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// setINIKey returns cfg with key=value set inside section (e.g. "[web]"),
// replacing the existing line, inserting it right after the section header
// when the key is missing (adopted ISPConfig schemas predate it) or
// appending the whole section when that is missing too.
func setINIKey(cfg, section, key, value string) string {
	line := key + "=" + value
	re := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=.*$`)
	if re.MatchString(cfg) {
		return re.ReplaceAllString(cfg, line)
	}
	if i := strings.Index(cfg, section); i >= 0 {
		j := i + len(section)
		return cfg[:j] + "\n" + line + cfg[j:]
	}
	return strings.TrimRight(cfg, "\n") + "\n\n" + section + "\n" + line + "\n"
}

// updateServerConfig applies keys to the [section] of the local server row's
// config INI. It is a no-op when nothing changed, so every step calling it
// stays idempotent across re-runs.
func updateServerConfig(db *gorm.DB, hostname, section string, keys map[string]string) error {
	var srv model.Server
	if err := db.Where("server_name = ?", hostname).Order("server_id").Take(&srv).Error; err != nil {
		return fmt.Errorf("loading server row %q: %w", hostname, err)
	}
	updated := srv.Config
	for key, value := range keys {
		updated = setINIKey(updated, section, key, value)
	}
	if updated == srv.Config {
		return nil
	}
	return db.Model(&model.Server{}).
		Where("server_id = ?", srv.ServerID).
		Update("config", updated).Error
}
