package transport

// ProtocolVersion is the wire protocol an agent advertises in Hello.
//
//	1 — control + data streams.
//	2 — adds EnableSnaps (hub→agent) and the agent-opened snapshot stream.
//	3 — adds SessionStat.activity in Heartbeat (additive; a v1/v2 peer
//	    ignores the unknown field). The supported window is now [1,3]; the
//	    addition is backward compatible.
//	4 — adds Heartbeat.metrics (host CPU/RAM; additive, older peers ignore it).
//	    The supported window is now [1,4]; backward compatible.
//
// The snapshot additions are backward compatible: a v1 agent never opens a
// snapshot stream and ignores EnableSnaps, and a v1 hub never sends EnableSnaps,
// so a mixed v1/v2 pair interoperates (just without the overview feed).
const ProtocolVersion = 4

const (
	MinSupportedProtocol = 1
	MaxSupportedProtocol = 4
)

func ProtocolSupported(v int) bool {
	return v >= MinSupportedProtocol && v <= MaxSupportedProtocol
}
