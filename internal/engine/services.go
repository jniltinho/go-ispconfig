package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
)

// Service actions accepted by the services registry.
const (
	// ActionRestart fully restarts a service.
	ActionRestart = "restart"
	// ActionReload asks a service to reload its configuration.
	ActionReload = "reload"
)

// Executor runs a service action on the operating system. The production
// implementation shells out to systemctl; tests inject a mock.
type Executor interface {
	// Run performs action ("restart"/"reload") on the named systemd unit.
	Run(ctx context.Context, service, action string) error
}

// SystemctlExecutor is the production Executor: systemctl <action> <unit>.
type SystemctlExecutor struct{}

// Run executes systemctl with the requested action and unit name.
func (SystemctlExecutor) Run(ctx context.Context, service, action string) error {
	out, err := exec.CommandContext(ctx, "systemctl", action, service).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %w: %s", action, service, err, out)
	}
	return nil
}

// Services is the delayed service restart registry (port of
// services.inc.php): plugins request restart/reload during datalog
// processing, requests are deduplicated per service with reload upgraded to
// restart, and everything executes once at the end of the daemon cycle.
type Services struct {
	log        *slog.Logger
	exec       Executor
	mu         sync.Mutex
	registered map[string]struct{}
	delayed    map[string]string // service -> pending action
}

// NewServices creates a Services registry executing through exec and
// logging through log (nil means slog.Default).
func NewServices(exec Executor, log *slog.Logger) *Services {
	if log == nil {
		log = slog.Default()
	}
	return &Services{
		log:        log,
		exec:       exec,
		registered: map[string]struct{}{},
		delayed:    map[string]string{},
	}
}

// Register declares a restartable service by its systemd unit name (port of
// registerService); only registered services accept delayed requests.
func (s *Services) Register(service string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[service] = struct{}{}
}

// RestartServiceDelayed queues action for service until the end of the
// current daemon cycle (port of restartServiceDelayed). Duplicate requests
// collapse into one; a restart request wins over a queued reload and a later
// reload never downgrades a queued restart. Unregistered services are
// ignored with a warning, PHP parity.
func (s *Services) RestartServiceDelayed(service, action string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.registered[service]; !ok {
		s.log.Warn("engine: delayed restart for unregistered service ignored", "service", service, "action", action)
		return
	}
	if s.delayed[service] == ActionRestart {
		return // never downgrade restart to reload
	}
	s.delayed[service] = action
}

// ProcessDelayedActions executes every queued action exactly once and clears
// the queue (port of processDelayedActions, called at the end of a daemon
// cycle). Execution failures are logged, not returned: one broken service
// must not block the others.
func (s *Services) ProcessDelayedActions(ctx context.Context) {
	s.mu.Lock()
	pending := s.delayed
	s.delayed = map[string]string{}
	s.mu.Unlock()

	for service, action := range pending {
		s.log.Info("engine: executing delayed service action", "service", service, "action", action)
		if err := s.exec.Run(ctx, service, action); err != nil {
			s.log.Error("engine: service action failed", "service", service, "action", action, "error", err)
		}
	}
}
