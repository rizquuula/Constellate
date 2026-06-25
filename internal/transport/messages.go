package transport

// MessageType identifies the kind of wire message.
type MessageType string

const (
	TypeHello         MessageType = "Hello"
	TypeHeartbeat     MessageType = "Heartbeat"
	TypeError         MessageType = "Error"
	TypeSessionOpened MessageType = "SessionOpened"
	TypeSessionExited MessageType = "SessionExited"
	TypeOpenSession   MessageType = "OpenSession"
	TypeResize        MessageType = "Resize"
	TypeCloseSession  MessageType = "CloseSession"
	TypeEnableSnaps   MessageType = "EnableSnaps"
	TypeAttach        MessageType = "Attach"
	TypeSnapStream    MessageType = "SnapStream"
	TypeSnapshot      MessageType = "Snapshot"
)

// --- agent → hub ---

// Hello is the first message sent by an agent after connecting.
type Hello struct {
	Type            MessageType `json:"type"`
	MachineID       string      `json:"machineID"`
	InstanceID      string      `json:"instanceID"`
	Name            string      `json:"name"`
	OS              string      `json:"os"`
	Arch            string      `json:"arch"`
	AgentVersion    string      `json:"agentVersion"`
	ProtocolVersion int         `json:"protocolVersion"`
}

// SessionStat carries per-session status within a Heartbeat.
type SessionStat struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	BytesOut int64  `json:"bytesOut"`
	Activity string `json:"activity,omitempty"`
}

// Metrics carries host-level resource usage sampled by the agent.
// CPUPercent is the host CPU utilisation over the last heartbeat interval.
// It is set to -1 when unavailable. MemUsedMB and MemTotalMB are in mebibytes.
type Metrics struct {
	CPUPercent float64 `json:"cpuPercent"`
	MemUsedMB  int64   `json:"memUsedMB"`
	MemTotalMB int64   `json:"memTotalMB"`
}

// Heartbeat is sent periodically by the agent to confirm liveness.
type Heartbeat struct {
	Type     MessageType   `json:"type"`
	TS       int64         `json:"ts"`
	Sessions []SessionStat `json:"sessions"`
	Metrics  *Metrics      `json:"metrics,omitempty"`
}

// SessionOpened is sent by the agent when a requested session is ready.
type SessionOpened struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
	PID       int         `json:"pid"`
}

// SessionExited is sent by the agent when a session's process exits.
type SessionExited struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
	ExitCode  int         `json:"exitCode"`
}

// Error is sent by the agent to report a problem.
type Error struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID,omitempty"`
	Code      string      `json:"code"`
	Message   string      `json:"message"`
}

// --- hub → agent ---

// OpenSession instructs the agent to start a new terminal session.
type OpenSession struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
	Cwd       string      `json:"cwd"`
	Shell     string      `json:"shell"`
	Cols      int         `json:"cols"`
	Rows      int         `json:"rows"`
	// CreateDir asks the agent to create Cwd (recursively) if it is missing
	// before starting the shell. Additive/optional field — older agents ignore
	// it and report cwd_not_found, so no protocol bump is required.
	CreateDir bool `json:"createDir,omitempty"`
	// Revive signals that this open is a restart-revival: the hub is
	// re-opening a session that was running before an agent process restart.
	// The agent already replays scrollback on any Open with a known sessionID
	// (Phase-1 behaviour); this flag is an intent/audit hint. Additive —
	// agents on protocol ≤4 ignore it via JSON omitempty on their decoders.
	Revive bool `json:"revive,omitempty"`
}

// Resize instructs the agent to resize an existing session's PTY.
type Resize struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
	Cols      int         `json:"cols"`
	Rows      int         `json:"rows"`
}

// CloseSession instructs the agent to terminate a session.
type CloseSession struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
}

// EnableSnaps instructs the agent to start (or stop) producing overview
// snapshots. The hub enables snapshots while at least one browser is watching
// the overview and disables them when the last viewer leaves, so the snapshot
// stream costs nothing when nobody is watching.
type EnableSnaps struct {
	Type    MessageType `json:"type"`
	Enabled bool        `json:"enabled"`
}

// --- constructors ---

// NewHello constructs a Hello message with the Type field pre-set.
func NewHello(machineID, instanceID, name, os, arch, agentVersion string, protocolVersion int) Hello {
	return Hello{
		Type:            TypeHello,
		MachineID:       machineID,
		InstanceID:      instanceID,
		Name:            name,
		OS:              os,
		Arch:            arch,
		AgentVersion:    agentVersion,
		ProtocolVersion: protocolVersion,
	}
}

// NewHeartbeat constructs a Heartbeat message with the Type field pre-set.
// metrics may be nil when host sampling is unavailable or not wired.
func NewHeartbeat(ts int64, sessions []SessionStat, metrics *Metrics) Heartbeat {
	return Heartbeat{
		Type:     TypeHeartbeat,
		TS:       ts,
		Sessions: sessions,
		Metrics:  metrics,
	}
}

// NewError constructs an Error message with the Type field pre-set.
func NewError(sessionID, code, message string) Error {
	return Error{
		Type:      TypeError,
		SessionID: sessionID,
		Code:      code,
		Message:   message,
	}
}

// NewSessionOpened constructs a SessionOpened message with the Type field pre-set.
func NewSessionOpened(sessionID string, pid int) SessionOpened {
	return SessionOpened{
		Type:      TypeSessionOpened,
		SessionID: sessionID,
		PID:       pid,
	}
}

// NewSessionExited constructs a SessionExited message with the Type field pre-set.
func NewSessionExited(sessionID string, exitCode int) SessionExited {
	return SessionExited{
		Type:      TypeSessionExited,
		SessionID: sessionID,
		ExitCode:  exitCode,
	}
}

// NewOpenSession constructs an OpenSession message with the Type field pre-set.
func NewOpenSession(sessionID, cwd, shell string, cols, rows int, createDir, revive bool) OpenSession {
	return OpenSession{
		Type:      TypeOpenSession,
		SessionID: sessionID,
		Cwd:       cwd,
		Shell:     shell,
		Cols:      cols,
		Rows:      rows,
		CreateDir: createDir,
		Revive:    revive,
	}
}

// NewResize constructs a Resize message with the Type field pre-set.
func NewResize(sessionID string, cols, rows int) Resize {
	return Resize{
		Type:      TypeResize,
		SessionID: sessionID,
		Cols:      cols,
		Rows:      rows,
	}
}

// NewCloseSession constructs a CloseSession message with the Type field pre-set.
func NewCloseSession(sessionID string) CloseSession {
	return CloseSession{
		Type:      TypeCloseSession,
		SessionID: sessionID,
	}
}

// NewEnableSnaps constructs an EnableSnaps message with the Type field pre-set.
func NewEnableSnaps(enabled bool) EnableSnaps {
	return EnableSnaps{
		Type:    TypeEnableSnaps,
		Enabled: enabled,
	}
}
