package web

import (
	"context"
	"fmt"

	"go-ispconfig/internal/engine"
)

// HttpdService is the service key plugins use for delayed nginx
// reload/restart requests (PHP parity: services.inc.php registers 'httpd').
const HttpdService = "httpd"

// nginxUnit is the systemd unit behind the httpd service key.
const nginxUnit = "nginx"

// GuardedExecutor wraps the services Executor with the web-module service
// semantics (port of web_module.inc.php restartHttpd/restartPHP_FPM):
//
//   - the "httpd" service key maps to the nginx systemd unit and every
//     restart/reload runs `nginx -t` first — a failed configuration test
//     aborts the action and surfaces the nginx output as the error, so a
//     broken vhost can never take nginx down;
//   - any other service (the per-PHP-version FPM units like "php8.3-fpm")
//     passes through unchanged.
type GuardedExecutor struct {
	// Inner performs the actual service action (systemctl in production).
	Inner engine.Executor
	// Runner executes nginx -t.
	Runner engine.CommandRunner
}

// Run implements engine.Executor.
func (e GuardedExecutor) Run(ctx context.Context, service, action string) error {
	if service != HttpdService {
		return e.Inner.Run(ctx, service, action)
	}
	if out, err := e.Runner.Run(ctx, "nginx", "-t"); err != nil {
		return fmt.Errorf("web: nginx -t failed, %s aborted: %w: %s", action, err, out)
	}
	return e.Inner.Run(ctx, nginxUnit, action)
}

// RegisterServices declares the web services in the delayed-restart registry:
// the guarded httpd service and one php-fpm service per known FPM unit (the
// server default plus each server_php init script). Delayed requests are
// deduplicated per unit by the registry, so two pool changes on the same PHP
// version reload that FPM exactly once and other versions are not touched.
func RegisterServices(s *engine.Services, fpmUnits ...string) {
	s.Register(HttpdService)
	for _, unit := range fpmUnits {
		if unit != "" {
			s.Register(unit)
		}
	}
}
