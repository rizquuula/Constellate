package transport

// Local-protocol message types (connect ⇄ host over UDS).
// They live in the same transport package so they share the NDJSON codec and
// type-tag dispatch without a second encoding layer.
const (
	TypeHostHello    MessageType = "HostHello"
	TypeHostInfo     MessageType = "HostInfo"
	TypeListSessions MessageType = "ListSessions"
	// TypeLocalStat carries per-session activity signals from the host to
	// connect for folding into the hub Heartbeat. Added in local protocol v2.
	TypeLocalStat MessageType = "LocalStat"
)

// HostHello is the first frame sent by connect on the local control stream.
// It carries the connect's view of the local protocol version so the host can
// negotiate the minimum supported feature set.
type HostHello struct {
	Type          MessageType `json:"type"`
	LocalProtocol int         `json:"localProtocol"`
}

// HostInfo is the reply from the host to a HostHello. It carries the host's
// instanceID (stable for the life of the host process), the negotiated local
// protocol version, and the list of currently open sessions.
type HostInfo struct {
	Type          MessageType   `json:"type"`
	InstanceID    string        `json:"instanceID"`
	LocalProtocol int           `json:"localProtocol"`
	Sessions      []SessionStub `json:"sessions"`
}

// SessionStub is a lightweight descriptor of an open session returned in
// HostInfo. It carries only the fields connect needs to inform the hub.
type SessionStub struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
}

// ListSessions is an optional explicit request from connect to re-list the
// current sessions (useful after a reconnect if HostInfo is stale).
type ListSessions struct {
	Type MessageType `json:"type"`
}

// LocalSessionActivity is a per-session activity signal shipped from host to
// connect in a LocalStat frame. It mirrors terminal.SessionActivity but lives
// in transport so no domain type crosses the wire boundary.
//
// Pwd is relayed because the PTYs live in the host process; connect cannot read
// a session's working directory itself. Added in local protocol v3.
type LocalSessionActivity struct {
	ID       string `json:"id"`
	Activity string `json:"activity"`
	Pwd      string `json:"pwd,omitempty"`
}

// LocalStat is a periodic frame sent by the host → connect over the local
// control stream. It carries per-session activity signals that connect folds
// into the hub Heartbeat. Added in local protocol v2.
type LocalStat struct {
	Type       MessageType            `json:"type"`
	Activities []LocalSessionActivity `json:"activities"`
}

// --- constructors ---

// NewHostHello constructs a HostHello with the Type field pre-set.
func NewHostHello(localProtocol int) HostHello {
	return HostHello{
		Type:          TypeHostHello,
		LocalProtocol: localProtocol,
	}
}

// NewHostInfo constructs a HostInfo with the Type field pre-set.
func NewHostInfo(instanceID string, localProtocol int, sessions []SessionStub) HostInfo {
	return HostInfo{
		Type:          TypeHostInfo,
		InstanceID:    instanceID,
		LocalProtocol: localProtocol,
		Sessions:      sessions,
	}
}

// NewListSessions constructs a ListSessions with the Type field pre-set.
func NewListSessions() ListSessions {
	return ListSessions{Type: TypeListSessions}
}

// NewLocalStat constructs a LocalStat with the Type field pre-set.
func NewLocalStat(activities []LocalSessionActivity) LocalStat {
	return LocalStat{Type: TypeLocalStat, Activities: activities}
}
