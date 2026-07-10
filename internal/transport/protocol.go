package transport

// LocalProtocolVersion is the version of the connect⇄host local (UDS) protocol.
//
//	1 — initial version: HostHello/HostInfo handshake, OpenSession/Resize/
//	    CloseSession/EnableSnaps forwarding, AttachHeader+raw data streams.
//	2 — adds LocalStat (host→connect activity) and host-side snapshot production
//	    (host opens snapshot stream; connect relays to hub). EnableSnaps is now
//	    forwarded to the host's snapshot toggle.
//	3 — adds LocalSessionActivity.pwd. Provenance only: LocalStat already ships
//	    at the >= 2 gate and pwd is additive JSON, so no negotiated behaviour
//	    depends on v3 and a v2/v3 pair interoperates in both directions.
const LocalProtocolVersion = 3

// ProtocolVersion is the wire protocol an agent advertises in Hello.
//
//	1 — control + data streams.
//	2 — adds EnableSnaps (hub→agent) and the agent-opened snapshot stream.
//	3 — adds SessionStat.activity in Heartbeat (additive; a v1/v2 peer
//	    ignores the unknown field). The supported window is now [1,3]; the
//	    addition is backward compatible.
//	4 — adds Heartbeat.metrics (host CPU/RAM; additive, older peers ignore it).
//	    The supported window is now [1,4]; backward compatible.
//	5 — adds OpenSession.Revive for restart auto-relaunch (hub hint; additive,
//	    older agents ignore the unknown field via JSON omitempty semantics).
//	    The supported window is now [1,5]; backward compatible.
//	6 — adds SessionStat.pwd in Heartbeat (the session's live working directory,
//	    distinct from the fixed OpenSession.Cwd; additive, a ≤v5 hub ignores the
//	    unknown field). The supported window is now [1,6]; backward compatible.
//
// The snapshot additions are backward compatible: a v1 agent never opens a
// snapshot stream and ignores EnableSnaps, and a v1 hub never sends EnableSnaps,
// so a mixed v1/v2 pair interoperates (just without the overview feed).
//
// Note the compatibility is one-directional: an agent advertises a single
// ProtocolVersion rather than a range, so a new agent is rejected by an old hub
// whose window has not yet widened. Upgrade the hub before the agents.
const ProtocolVersion = 6

const (
	MinSupportedProtocol = 1
	MaxSupportedProtocol = 6
)

func ProtocolSupported(v int) bool {
	return v >= MinSupportedProtocol && v <= MaxSupportedProtocol
}
