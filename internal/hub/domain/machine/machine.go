package machine

// Machine represents an enrolled agent machine. Fields are unexported;
// use constructors and accessors.
type Machine struct {
	id           string
	instanceID   string
	name         string
	os           string
	arch         string
	agentVersion string
	enrolledAt   int64
	lastSeenAt   int64
	revokedAt    int64 // 0 = not revoked
}

// New creates a Machine at enrollment time. lastSeenAt is set equal to enrolledAt.
func New(id, instanceID, name, osName, arch, agentVersion string, enrolledAt int64) Machine {
	return Machine{
		id:           id,
		instanceID:   instanceID,
		name:         name,
		os:           osName,
		arch:         arch,
		agentVersion: agentVersion,
		enrolledAt:   enrolledAt,
		lastSeenAt:   enrolledAt,
	}
}

// Rehydrate reconstructs a Machine from a persisted row.
func Rehydrate(id, instanceID, name, osName, arch, agentVersion string, enrolledAt, lastSeenAt int64) Machine {
	return Machine{
		id:           id,
		instanceID:   instanceID,
		name:         name,
		os:           osName,
		arch:         arch,
		agentVersion: agentVersion,
		enrolledAt:   enrolledAt,
		lastSeenAt:   lastSeenAt,
	}
}

// rehydrateFull reconstructs a Machine including the revoked_at field.
// Used internally by adapters that need to restore revocation state.
func rehydrateFull(id, instanceID, name, osName, arch, agentVersion string, enrolledAt, lastSeenAt, revokedAt int64) Machine {
	return Machine{
		id:           id,
		instanceID:   instanceID,
		name:         name,
		os:           osName,
		arch:         arch,
		agentVersion: agentVersion,
		enrolledAt:   enrolledAt,
		lastSeenAt:   lastSeenAt,
		revokedAt:    revokedAt,
	}
}

func (m Machine) ID() string           { return m.id }
func (m Machine) InstanceID() string   { return m.instanceID }
func (m Machine) Name() string         { return m.name }
func (m Machine) OS() string           { return m.os }
func (m Machine) Arch() string         { return m.arch }
func (m Machine) AgentVersion() string { return m.agentVersion }
func (m Machine) EnrolledAt() int64    { return m.enrolledAt }
func (m Machine) LastSeenAt() int64    { return m.lastSeenAt }
func (m Machine) RevokedAt() int64     { return m.revokedAt }
func (m Machine) Revoked() bool        { return m.revokedAt != 0 }

// MarkRevoked returns a copy of m with revokedAt set to ts.
func (m Machine) MarkRevoked(ts int64) Machine {
	m.revokedAt = ts
	return m
}

// ClearRevoked returns a copy of m with revokedAt reset to 0 (not revoked).
func (m Machine) ClearRevoked() Machine { m.revokedAt = 0; return m }

// Touch updates the last-seen timestamp.
func (m *Machine) Touch(ts int64) {
	m.lastSeenAt = ts
}

// RehydrateFull reconstructs a Machine including the revoked_at field.
// Used by adapters that persist and restore revocation state.
func RehydrateFull(id, instanceID, name, osName, arch, agentVersion string, enrolledAt, lastSeenAt, revokedAt int64) Machine {
	return rehydrateFull(id, instanceID, name, osName, arch, agentVersion, enrolledAt, lastSeenAt, revokedAt)
}
