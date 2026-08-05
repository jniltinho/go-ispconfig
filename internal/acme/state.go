package acme

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultStatePath is the per-node issuance cache (design: idempotence and
// operator visibility). Not in the database — it is local daemon state.
const DefaultStatePath = "/var/lib/go-ispconfig/acme/state.json"

// DomainState records the last outcome for one main domain.
type DomainState struct {
	Provider    string    `json:"provider,omitempty"`
	LastRenewal time.Time `json:"last_renewal,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

type stateFile struct {
	Domains map[string]DomainState `json:"domains"`
}

// StateStore persists domain → outcome on disk.
type StateStore struct {
	path string
	mu   sync.Mutex
	log  *slog.Logger
}

// NewStateStore returns a store at path (DefaultStatePath when empty).
func NewStateStore(path string) *StateStore {
	if path == "" {
		path = DefaultStatePath
	}
	return &StateStore{path: path, log: slog.Default()}
}

// SetLogger routes this store's diagnostics; nil restores the default.
func (s *StateStore) SetLogger(l *slog.Logger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l == nil {
		l = slog.Default()
	}
	s.log = l
}

func (s *StateStore) loadLocked() stateFile {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return stateFile{Domains: map[string]DomainState{}}
	}
	var f stateFile
	if err := json.Unmarshal(raw, &f); err != nil {
		// A truncated or hand-edited file must be loud: read as empty it
		// silently forgets every domain's provider.
		s.log.Error("acme: state file unreadable, treating as empty",
			"path", s.path, "error", err)
		return stateFile{Domains: map[string]DomainState{}}
	}
	if f.Domains == nil {
		return stateFile{Domains: map[string]DomainState{}}
	}
	return f
}

func (s *StateStore) saveLocked(f stateFile) error {
	if f.Domains == nil {
		f.Domains = map[string]DomainState{}
	}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	// 0600: last_error carries whatever the CA or the DNS provider said, which
	// is not worth making world-readable on a shared host.
	return writeFileAtomic(s.path, raw, 0o600)
}

// RecordSuccess updates last_renewal for domain.
func (s *StateStore) RecordSuccess(domain, provider string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.loadLocked()
	st := f.Domains[domain]
	st.Provider = provider
	st.LastRenewal = at
	st.LastError = ""
	f.Domains[domain] = st
	if err := s.saveLocked(f); err != nil {
		// Swallowing this loses the provider choice, and the next renewal then
		// falls back to http-01 for a domain that needs dns-01 — a wildcard
		// that fails with no trace of why.
		s.log.Error("acme: writing state", "path", s.path, "error", err)
	}
}

// RecordError stores the last failure for domain.
func (s *StateStore) RecordError(domain, provider, cause string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.loadLocked()
	st := f.Domains[domain]
	st.Provider = provider
	st.LastError = cause
	f.Domains[domain] = st
	if err := s.saveLocked(f); err != nil {
		// Swallowing this loses the provider choice, and the next renewal then
		// falls back to http-01 for a domain that needs dns-01 — a wildcard
		// that fails with no trace of why.
		s.log.Error("acme: writing state", "path", s.path, "error", err)
	}
}

// Get returns the stored state for domain (zero when unknown).
func (s *StateStore) Get(domain string) DomainState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked().Domains[domain]
}
