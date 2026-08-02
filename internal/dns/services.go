package dns

import (
	"context"
	"os"
	"sync"

	"go-ispconfig/internal/engine"
)

// BindService is the service key plugins use for delayed bind
// reload/restart requests (PHP parity: dns_module registers 'bind').
const BindService = "bind"

// systemdUnitDirs are the locations checked for a unit file when resolving
// the bind unit name (port of the init-script existence check in
// dns_module.inc.php restartBind).
var systemdUnitDirs = []string{
	"/etc/systemd/system",
	"/lib/systemd/system",
	"/usr/lib/systemd/system",
}

// BindExecutor wraps the services Executor with the dns-module service
// semantics: the "bind" service key maps to the systemd unit "bind9" when
// such a unit exists (Debian/Ubuntu), otherwise "named". Any other service
// passes through unchanged. The unit is resolved once per daemon run.
type BindExecutor struct {
	// Inner performs the actual service action (systemctl in production).
	Inner engine.Executor
	// UnitExists reports whether a systemd unit exists; nil means a unit
	// file lookup in the standard systemd directories (tests inject a fake).
	UnitExists func(unit string) bool

	once sync.Once
	unit string
}

// Run implements engine.Executor.
func (e *BindExecutor) Run(ctx context.Context, service, action string) error {
	if service != BindService {
		return e.Inner.Run(ctx, service, action)
	}
	e.once.Do(func() {
		exists := e.UnitExists
		if exists == nil {
			exists = unitFileExists
		}
		e.unit = "named"
		if exists("bind9") {
			e.unit = "bind9"
		}
	})
	return e.Inner.Run(ctx, e.unit, action)
}

// unitFileExists checks the standard systemd directories for <unit>.service.
func unitFileExists(unit string) bool {
	for _, dir := range systemdUnitDirs {
		if _, err := os.Stat(dir + "/" + unit + ".service"); err == nil {
			return true
		}
	}
	return false
}

// RegisterServices declares the bind service in the delayed-restart
// registry so plugins can queue restart/reload requests for it.
func RegisterServices(s *engine.Services) {
	s.Register(BindService)
}
