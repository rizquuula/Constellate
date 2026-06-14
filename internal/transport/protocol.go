package transport

// ProtocolVersion is the wire protocol an agent advertises in Hello.
//
//	1 — M0–M3: control + data streams.
//	2 — M4: adds EnableSnaps (hub→agent) and the agent-opened snapshot stream.
//
// The M4 additions are backward compatible: a v1 agent never opens a snapshot
// stream and ignores EnableSnaps, and a v1 hub never sends EnableSnaps, so a
// mixed v1/v2 pair interoperates (just without the overview feed).
const ProtocolVersion = 2

const (
	MinSupportedProtocol = 1
	MaxSupportedProtocol = 2
)

func ProtocolSupported(v int) bool {
	return v >= MinSupportedProtocol && v <= MaxSupportedProtocol
}
