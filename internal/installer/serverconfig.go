package installer

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/getconf"
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

// localServer resolves this host's server row. The row is seeded with
// whatever hostname the install ran with, which is not always what a later
// run reports: a host installed as "apache-test.goisp.test" answers
// "apache-test" once its domain search is set up. So an exact match is tried
// first, then the FQDN of that short name, then — on a single-server install
// — the only row there is.
func localServer(db *gorm.DB, hostname string) (model.Server, error) {
	var srv model.Server
	err := db.Where("server_name = ?", hostname).Order("server_id").Take(&srv).Error
	if err == nil {
		return srv, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return srv, fmt.Errorf("loading server row %q: %w", hostname, err)
	}
	short, _, _ := strings.Cut(hostname, ".")
	if err := db.Where("server_name = ? OR server_name LIKE ?", short, short+".%").
		Order("server_id").Take(&srv).Error; err == nil {
		return srv, nil
	}

	var servers []model.Server
	if err := db.Order("server_id").Find(&servers).Error; err != nil {
		return srv, fmt.Errorf("loading the server table: %w", err)
	}
	if len(servers) != 1 {
		return srv, fmt.Errorf("no server row matches %q and the server table holds %d rows", hostname, len(servers))
	}
	return servers[0], nil
}

// mailRole resolves this host's server id together with its [mail] config,
// returning Skip when the step must not run: no database yet, or no mail
// role on this host. Every mail step (vmail, postfix, dovecot, getmail)
// opens with it.
func mailRole(st *State) (uint32, getconf.MailConfig, error) {
	if st.DB == nil {
		return 0, getconf.MailConfig{}, Skip("no database connection")
	}
	srv, err := localServer(st.DB, st.Answers.Hostname)
	if err != nil {
		return 0, getconf.MailConfig{}, err
	}
	if srv.MailServer != 1 {
		return 0, getconf.MailConfig{}, Skip("mail role disabled")
	}
	// GetServerConfig seeds the defaults before decoding, so a server row
	// without a [mail] section still yields a usable config.
	cfg := getconf.DefaultMailConfig()
	if sc, err := getconf.GetServerConfig(st.DB, srv.ServerID); err == nil {
		cfg = sc.Mail
	}
	return srv.ServerID, cfg, nil
}

// updateServerConfig applies keys to the [section] of the local server row's
// config INI. It is a no-op when nothing changed, so every step calling it
// stays idempotent across re-runs.
func updateServerConfig(db *gorm.DB, hostname, section string, keys map[string]string) error {
	srv, err := localServer(db, hostname)
	if err != nil {
		return err
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
