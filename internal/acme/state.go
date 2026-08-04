package acme

import (
	"encoding/json"
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
}

// NewStateStore returns a store at path (DefaultStatePath when empty).
func NewStateStore(path string) *StateStore {
	if path == "" {
		path = DefaultStatePath
	}
	return &StateStore{path: path}
}

func (s *StateStore) loadLocked() stateFile {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return stateFile{Domains: map[string]DomainState{}}
	}
	var f stateFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Domains == nil {
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
	return writeFileAtomic(s.path, raw, 0o644)
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
	_ = s.saveLocked(f)
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
	_ = s.saveLocked(f)
}

// Get returns the stored state for domain (zero when unknown).
func (s *StateStore) Get(domain string) DomainState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked().Domains[domain]
}
