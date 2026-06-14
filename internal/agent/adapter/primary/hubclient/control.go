package hubclient

import (
	"log/slog"
	"runtime"

	"github.com/rizquuula/Constellate/internal/platform/version"
	"github.com/rizquuula/Constellate/internal/transport"
)

// sendHello encodes and sends a Hello frame on enc.
func sendHello(enc *transport.Encoder, machineID, name string) error {
	return enc.Encode(transport.NewHello(
		machineID,
		name,
		runtime.GOOS,
		runtime.GOARCH,
		version.Version,
		transport.ProtocolVersion,
	))
}

// handleFrame processes an inbound frame from the hub on the control stream.
// Currently only Error frames are acted on; all other types are silently ignored
// (they are not expected on the control stream in M0).
func handleFrame(frame transport.Frame, machineID string, log *slog.Logger) {
	if frame.Type != transport.TypeError {
		return
	}
	e, err := transport.Unmarshal[transport.Error](frame)
	if err != nil {
		log.Warn("hub error: could not decode error frame", "machineID", machineID, "err", err)
		return
	}
	log.Warn("hub error", "machineID", machineID, "code", e.Code, "message", e.Message)
}
