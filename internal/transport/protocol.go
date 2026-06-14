package transport

const ProtocolVersion = 1

const (
	MinSupportedProtocol = 1
	MaxSupportedProtocol = 1
)

func ProtocolSupported(v int) bool {
	return v >= MinSupportedProtocol && v <= MaxSupportedProtocol
}
