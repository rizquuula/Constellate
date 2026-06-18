package transport_test

import (
	"bytes"
	"testing"

	"github.com/rizquuula/Constellate/internal/transport"
)

// TestLocalMessageRoundTrip verifies that HostHello, HostInfo, and ListSessions
// survive a marshal/unmarshal cycle via the NDJSON codec, matching the style of
// the existing codec_test.go.
func TestLocalMessageRoundTrip(t *testing.T) {
	t.Run("HostHello", func(t *testing.T) {
		msg := transport.NewHostHello(transport.LocalProtocolVersion)
		if msg.Type != transport.TypeHostHello {
			t.Errorf("Type: got %q, want %q", msg.Type, transport.TypeHostHello)
		}
		if msg.LocalProtocol != transport.LocalProtocolVersion {
			t.Errorf("LocalProtocol: got %d, want %d", msg.LocalProtocol, transport.LocalProtocolVersion)
		}

		var buf bytes.Buffer
		enc := transport.NewEncoder(&buf)
		if err := enc.Encode(msg); err != nil {
			t.Fatalf("encode: %v", err)
		}

		dec := transport.NewDecoder(&buf)
		frame, err := dec.Next()
		if err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if frame.Type != transport.TypeHostHello {
			t.Errorf("frame.Type: got %q, want %q", frame.Type, transport.TypeHostHello)
		}
		got, err := transport.Unmarshal[transport.HostHello](frame)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.LocalProtocol != transport.LocalProtocolVersion {
			t.Errorf("LocalProtocol after round-trip: got %d, want %d",
				got.LocalProtocol, transport.LocalProtocolVersion)
		}
	})

	t.Run("HostInfo", func(t *testing.T) {
		sessions := []transport.SessionStub{
			{ID: "s1", PID: 100},
			{ID: "s2", PID: 200},
		}
		msg := transport.NewHostInfo("inst-xyz", transport.LocalProtocolVersion, sessions)
		if msg.Type != transport.TypeHostInfo {
			t.Errorf("Type: got %q, want %q", msg.Type, transport.TypeHostInfo)
		}

		var buf bytes.Buffer
		enc := transport.NewEncoder(&buf)
		if err := enc.Encode(msg); err != nil {
			t.Fatalf("encode: %v", err)
		}

		dec := transport.NewDecoder(&buf)
		frame, err := dec.Next()
		if err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		got, err := transport.Unmarshal[transport.HostInfo](frame)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.InstanceID != "inst-xyz" {
			t.Errorf("InstanceID: got %q, want %q", got.InstanceID, "inst-xyz")
		}
		if got.LocalProtocol != transport.LocalProtocolVersion {
			t.Errorf("LocalProtocol: got %d, want %d", got.LocalProtocol, transport.LocalProtocolVersion)
		}
		if len(got.Sessions) != 2 {
			t.Fatalf("Sessions len: got %d, want 2", len(got.Sessions))
		}
		if got.Sessions[0].ID != "s1" || got.Sessions[0].PID != 100 {
			t.Errorf("Sessions[0]: got %+v", got.Sessions[0])
		}
		if got.Sessions[1].ID != "s2" || got.Sessions[1].PID != 200 {
			t.Errorf("Sessions[1]: got %+v", got.Sessions[1])
		}
	})

	t.Run("ListSessions", func(t *testing.T) {
		msg := transport.NewListSessions()
		if msg.Type != transport.TypeListSessions {
			t.Errorf("Type: got %q, want %q", msg.Type, transport.TypeListSessions)
		}

		var buf bytes.Buffer
		enc := transport.NewEncoder(&buf)
		if err := enc.Encode(msg); err != nil {
			t.Fatalf("encode: %v", err)
		}

		dec := transport.NewDecoder(&buf)
		frame, err := dec.Next()
		if err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		if frame.Type != transport.TypeListSessions {
			t.Errorf("frame.Type: got %q, want %q", frame.Type, transport.TypeListSessions)
		}
		got, err := transport.Unmarshal[transport.ListSessions](frame)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != transport.TypeListSessions {
			t.Errorf("Type after round-trip: got %q", got.Type)
		}
	})

	t.Run("LocalProtocolVersion_positive", func(t *testing.T) {
		if transport.LocalProtocolVersion < 1 {
			t.Errorf("LocalProtocolVersion must be >= 1, got %d", transport.LocalProtocolVersion)
		}
	})
}
