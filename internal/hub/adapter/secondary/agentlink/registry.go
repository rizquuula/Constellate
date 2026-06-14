package agentlink

import (
	"sync"

	"github.com/hashicorp/yamux"
)

// Conn holds the live connection state for one enrolled agent.
type Conn struct {
	MachineID   string
	Session     *yamux.Session
	ConnectedAt int64
}

// Registry is the live machineID → connection map shared between
// the wsagent primary adapter (writes) and the registry use case's
// LiveAgents read port (reads).
type Registry struct {
	mu    sync.RWMutex
	conns map[string]*Conn
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{conns: make(map[string]*Conn)}
}

// Add registers a live connection for machineID, replacing any prior entry.
func (r *Registry) Add(machineID string, c *Conn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[machineID] = c
}

// Remove deletes the entry for machineID.
func (r *Registry) Remove(machineID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.conns, machineID)
}

// Get returns the Conn for machineID and whether it was found.
func (r *Registry) Get(machineID string) (*Conn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[machineID]
	return c, ok
}

// IsOnline reports whether machineID has a live connection.
// Satisfies registry.LiveAgents.
func (r *Registry) IsOnline(machineID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.conns[machineID]
	return ok
}

// OnlineIDs returns the IDs of all currently connected agents.
// Satisfies registry.LiveAgents.
func (r *Registry) OnlineIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.conns))
	for id := range r.conns {
		ids = append(ids, id)
	}
	return ids
}
