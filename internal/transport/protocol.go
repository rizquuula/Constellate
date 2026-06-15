package transport

// ProtocolVersion is the wire protocol an agent advertises in Hello.
//
//	1 — M0–M3: control + data streams.
//	2 — M4: adds EnableSnaps (hub→agent) and the agent-opened snapshot stream.
//	3 — M7: adds SessionStat.activity in Heartbeat (additive; a v1/v2 peer
//	    ignores the unknown field). The supported window is now [1,3]; the
//	    addition is backward compatible.
//
// The M4 additions are backward compatible: a v1 agent never opens a snapshot
// stream and ignores EnableSnaps, and a v1 hub never sends EnableSnaps, so a
// mixed v1/v2 pair interoperates (just without the overview feed).
const ProtocolVersion = 3

const (
	MinSupportedProtocol = 1
	MaxSupportedProtocol = 3
)

func ProtocolSupported(v int) bool {
	return v >= MinSupportedProtocol && v <= MaxSupportedProtocol
}
