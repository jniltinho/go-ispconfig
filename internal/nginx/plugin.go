package nginx

import (
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// DefaultLogBaseDir is where per-domain nginx logs live (ISPConfig layout,
// referenced by the vhost template's error_log/access_log lines).
const DefaultLogBaseDir = "/var/log/ispconfig/httpd"

// Plugin is the nginx server plugin. It subscribes to the events announced
// by the web module and applies them to the filesystem, nginx and PHP-FPM.
type Plugin struct {
	db       *gorm.DB
	services *engine.Services
	runner   engine.CommandRunner
	log      *slog.Logger

	// customTplDir is the mastertpl override directory (conf-custom parity).
	customTplDir string
	// logBaseDir is DefaultLogBaseDir in production, a temp dir in tests.
	logBaseDir string
}

// NewPlugin creates the nginx plugin. customTplDir may be empty (embedded
// templates only); log nil means slog.Default.
func NewPlugin(db *gorm.DB, services *engine.Services, runner engine.CommandRunner, customTplDir string, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{
		db:           db,
		services:     services,
		runner:       runner,
		log:          log,
		customTplDir: customTplDir,
		logBaseDir:   DefaultLogBaseDir,
	}
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "nginx" }

// OnLoad subscribes the plugin to the web module events (port of
// nginx_plugin.inc.php onLoad, nginx paths only). The ssl handler slot of
// the PHP dual registration (design D2) is added by the web-ssl tasks.
func (p *Plugin) OnLoad(r *engine.Registry) error {
	subs := map[string]engine.EventFunc{
		"web_domain_insert":      p.webDomainInsert,
		"web_domain_update":      p.webDomainUpdate,
		"web_domain_delete":      p.webDomainDelete,
		"web_folder_update":      p.webFolderUpdate,
		"web_folder_delete":      p.webFolderDelete,
		"web_folder_user_insert": p.webFolderUser,
		"web_folder_user_update": p.webFolderUser,
		"web_folder_user_delete": p.webFolderUser,
		"client_delete":          p.clientDelete,
	}
	for event, fn := range subs {
		if err := r.RegisterEvent(event, fn); err != nil {
			return err
		}
	}
	return nil
}

// webConfig loads the [web] section of this server's config.
func (p *Plugin) webConfig(serverID uint32) (*getconf.WebConfig, error) {
	cfg, err := getconf.GetServerConfig(p.db, serverID)
	if err != nil {
		return nil, fmt.Errorf("nginx: loading server %d web config: %w", serverID, err)
	}
	return &cfg.Web, nil
}
