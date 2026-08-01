// Package engine implements the sys_datalog processing engine (design D2/D3):
// the module/plugin registries with two-level event dispatch, the remote
// action registry, the delayed service restart registry, the internal job
// scheduler and the persistent daemon that drives them. It is the Go port of
// ISPConfig3's server/lib/classes/{modules,plugins,services}.inc.php plus
// server.php.
package engine

import (
	"errors"
	"fmt"
	"log/slog"
)

// Data is the decoded sys_datalog payload passed to table hooks and events:
// the {"old","new"} diff written by package datalog.
type Data struct {
	Old map[string]any `json:"old"`
	New map[string]any `json:"new"`
}

// TableHookFunc is a module callback for a table change (port of the
// modules.inc.php notification hook signature).
type TableHookFunc func(table, action string, data Data) error

// EventFunc is a plugin callback for a named event (port of the
// plugins.inc.php event subscription signature).
type EventFunc func(event string, data Data) error

// ActionFunc handles one sys_remoteaction dispatch. It returns the resulting
// state, one of StateOK, StateWarning or StateError; a non-nil error is
// treated as StateError.
type ActionFunc func(action, param string) (string, error)

// Remote action result states (sys_remoteaction.action_state values).
const (
	// StateOK means the action completed successfully.
	StateOK = "ok"
	// StateWarning means the action completed with warnings.
	StateWarning = "warning"
	// StateError means the action failed.
	StateError = "error"
)

// Module translates raw table changes into named events. OnLoad must
// announce the events the module provides (AnnounceEvents) and register its
// table hooks (RegisterTableHook), mirroring a mods-enabled module class.
type Module interface {
	// Name identifies the module in logs.
	Name() string
	// OnLoad registers the module's table hooks and announces its events.
	OnLoad(r *Registry) error
}

// Plugin reacts to named events announced by modules. OnLoad must subscribe
// via RegisterEvent (and optionally RegisterAction), mirroring a
// plugins-enabled plugin class.
type Plugin interface {
	// Name identifies the plugin in logs.
	Name() string
	// OnLoad subscribes the plugin to announced events and actions.
	OnLoad(r *Registry) error
}

// ErrEventNotAnnounced is returned by RegisterEvent when a plugin subscribes
// to an event no module announced — a startup error per the sys-datalog spec
// (stricter than PHP, which only logged it).
var ErrEventNotAnnounced = errors.New("engine: event not announced by any module")

// Registry holds the table-hook, announced-event, event-subscription and
// action-subscription maps (port of the modules/plugins class state).
// Populate it with Load before starting the daemon; it is not safe for
// concurrent mutation afterwards.
type Registry struct {
	log       *slog.Logger
	hooks     map[string][]TableHookFunc
	announced map[string]string // event name -> announcing module
	events    map[string][]EventFunc
	actions   map[string][]ActionFunc
}

// NewRegistry creates an empty Registry logging through log (nil means
// slog.Default).
func NewRegistry(log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		log:       log,
		hooks:     map[string][]TableHookFunc{},
		announced: map[string]string{},
		events:    map[string][]EventFunc{},
		actions:   map[string][]ActionFunc{},
	}
}

// Load runs OnLoad for every module first (announcing events, registering
// table hooks) and then for every plugin (subscribing to events), exactly
// like server.php loads mods-enabled before plugins-enabled. Any OnLoad
// error — including an unannounced event subscription — aborts startup.
func (r *Registry) Load(modules []Module, plugins []Plugin) error {
	for _, m := range modules {
		if err := m.OnLoad(r); err != nil {
			return fmt.Errorf("engine: loading module %s: %w", m.Name(), err)
		}
	}
	for _, p := range plugins {
		if err := p.OnLoad(r); err != nil {
			return fmt.Errorf("engine: loading plugin %s: %w", p.Name(), err)
		}
	}
	return nil
}

// RegisterTableHook subscribes fn to changes of table (port of
// modules.inc.php registerTableHook).
func (r *Registry) RegisterTableHook(table string, fn TableHookFunc) {
	r.hooks[table] = append(r.hooks[table], fn)
}

// AnnounceEvents declares the named events a module provides (port of
// plugins.inc.php announceEvents). Only announced events accept
// subscriptions.
func (r *Registry) AnnounceEvents(module string, events ...string) {
	for _, e := range events {
		r.announced[e] = module
	}
}

// RegisterEvent subscribes fn to a named event. It returns
// ErrEventNotAnnounced when no module announced the event, which Load turns
// into a startup failure.
func (r *Registry) RegisterEvent(event string, fn EventFunc) error {
	if _, ok := r.announced[event]; !ok {
		return fmt.Errorf("%w: %s", ErrEventNotAnnounced, event)
	}
	r.events[event] = append(r.events[event], fn)
	return nil
}

// RegisterAction subscribes fn to a named remote action (port of
// plugins.inc.php registerAction).
func (r *Registry) RegisterAction(action string, fn ActionFunc) {
	r.actions[action] = append(r.actions[action], fn)
}

// RaiseTableHook dispatches a decoded datalog change to every hook
// registered for the table (port of modules.inc.php raiseTableHook). Hook
// errors are joined and returned; remaining hooks still run.
func (r *Registry) RaiseTableHook(table, action string, data Data) error {
	var errs []error
	for _, fn := range r.hooks[table] {
		if err := fn(table, action, data); err != nil {
			errs = append(errs, fmt.Errorf("table hook %s/%s: %w", table, action, err))
		}
	}
	return errors.Join(errs...)
}

// RaiseEvent dispatches a named event to every subscribed plugin (port of
// plugins.inc.php raiseEvent). Subscriber errors are joined and returned;
// remaining subscribers still run.
func (r *Registry) RaiseEvent(event string, data Data) error {
	r.log.Debug("engine: raised event", "event", event)
	var errs []error
	for _, fn := range r.events[event] {
		if err := fn(event, data); err != nil {
			errs = append(errs, fmt.Errorf("event %s: %w", event, err))
		}
	}
	return errors.Join(errs...)
}

// RaiseAction dispatches a remote action to every subscribed handler and
// returns the most severe resulting state (ok < warning < error), the port
// of plugins.inc.php raiseAction state aggregation.
func (r *Registry) RaiseAction(action, param string) string {
	state := StateOK
	for _, fn := range r.actions[action] {
		s, err := fn(action, param)
		if err != nil {
			r.log.Error("engine: action handler failed", "action", action, "error", err)
			s = StateError
		}
		switch s {
		case StateError:
			state = StateError
		case StateWarning:
			if state != StateError {
				state = StateWarning
			}
		}
	}
	return state
}

// EventName builds the conventional event name a module raises for a table
// change: <table>_<insert|update|delete> from the datalog action i/u/d.
func EventName(table, action string) string {
	switch action {
	case "i":
		return table + "_insert"
	case "u":
		return table + "_update"
	default:
		return table + "_delete"
	}
}
