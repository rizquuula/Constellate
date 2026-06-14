package agentlink

import (
	"net"
	"sync"

	"github.com/hashicorp/yamux"
	"github.com/rizquuula/Constellate/internal/transport"
)

type openResult struct {
	pid int
	err error
}

// Conn holds the live connection state for one enrolled agent.
// session and ctrlEnc are unexported; gateway.go (same package) accesses them directly.
type Conn struct {
	MachineID   string
	ConnectedAt int64
	session     *yamux.Session
	ctrlEnc     *transport.Encoder
	mu          sync.Mutex
	pending     map[string]chan openResult
}

// NewConn constructs a Conn for machineID backed by sess and ctrlEnc.
func NewConn(machineID string, sess *yamux.Session, ctrlEnc *transport.Encoder, connectedAt int64) *Conn {
	return &Conn{
		MachineID:   machineID,
		ConnectedAt: connectedAt,
		session:     sess,
		ctrlEnc:     ctrlEnc,
		pending:     make(map[string]chan openResult),
	}
}

// sendControl encodes msg onto the control stream.
func (c *Conn) sendControl(msg any) error {
	return c.ctrlEnc.Encode(msg)
}

// awaitOpen registers a pending channel for sessionID and returns it.
func (c *Conn) awaitOpen(sessionID string) chan openResult {
	ch := make(chan openResult, 1)
	c.mu.Lock()
	c.pending[sessionID] = ch
	c.mu.Unlock()
	return ch
}

// cancelOpen removes a pending channel for sessionID.
func (c *Conn) cancelOpen(sessionID string) {
	c.mu.Lock()
	delete(c.pending, sessionID)
	c.mu.Unlock()
}

// ResolveOpen resolves a pending OpenSession by sessionID.
// It is exported so that wsagent (different package) can call it from the inbound loop.
func (c *Conn) ResolveOpen(sessionID string, pid int, err error) {
	c.mu.Lock()
	ch, ok := c.pending[sessionID]
	if ok {
		delete(c.pending, sessionID)
	}
	c.mu.Unlock()
	if ok {
		ch <- openResult{pid: pid, err: err}
	}
}

// EnableSnaps sends an EnableSnaps control message to this agent.
func (c *Conn) EnableSnaps(enabled bool) error {
	return c.sendControl(transport.NewEnableSnaps(enabled))
}

// openDataStream opens a new yamux stream to the agent, sends the attach header,
// and returns it as a net.Conn.
func (c *Conn) openDataStream(sessionID string) (net.Conn, error) {
	stream, err := c.session.OpenStream()
	if err != nil {
		return nil, err
	}
	if err := transport.NewEncoder(stream).Encode(transport.NewAttachHeader(sessionID)); err != nil {
		_ = stream.Close()
		return nil, err
	}
	return stream, nil
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
