package dns

import (
	"log/slog"
	"sync"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
)

// Plugin is the bind server plugin (port of bind_plugin.inc.php). It
// subscribes to the events announced by the dns module and applies them to
// the zone files, named.conf.local and the bind service.
type Plugin struct {
	db       *gorm.DB
	services *engine.Services
	runner   engine.CommandRunner
	log      *slog.Logger
	serverID uint32

	// customTplDir is the mastertpl override directory (conf-custom parity).
	customTplDir string

	// caaOnce/caaSupported cache the `named -v` probe for the daemon run
	// (design D3). Tests preset caaProbed.
	caaOnce      sync.Once
	caaSupported bool
	caaProbed    *bool

	// resignThreshold overrides DefaultResignThreshold for the dns_resign
	// job (0 means the default).
	resignThreshold time.Duration
}

// NewPlugin creates the bind plugin for one server. customTplDir may be
// empty (embedded templates only); log nil means slog.Default.
func NewPlugin(db *gorm.DB, services *engine.Services, runner engine.CommandRunner, customTplDir string, serverID uint32, log *slog.Logger) *Plugin {
	if log == nil {
		log = slog.Default()
	}
	return &Plugin{
		db:           db,
		services:     services,
		runner:       runner,
		log:          log,
		serverID:     serverID,
		customTplDir: customTplDir,
	}
}

// Name identifies the plugin in logs.
func (*Plugin) Name() string { return "bind" }
