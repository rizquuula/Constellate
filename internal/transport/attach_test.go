package transport

import (
	"bytes"
	"io"
	"testing"
)

func TestReadAttachHeader(t *testing.T) {
	var buf bytes.Buffer

	// Write the attach header as the first NDJSON line.
	if err := NewEncoder(&buf).Encode(NewAttachHeader("sid")); err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Write raw bytes after the header.
	buf.WriteString("hello")

	hdr, br, err := ReadAttachHeader(&buf)
	if err != nil {
		t.Fatalf("ReadAttachHeader: %v", err)
	}
	if hdr.Type != TypeAttach {
		t.Errorf("Type: got %q, want %q", hdr.Type, TypeAttach)
	}
	if hdr.SessionID != "sid" {
		t.Errorf("SessionID: got %q, want %q", hdr.SessionID, "sid")
	}

	// The returned reader must yield exactly the raw bytes that follow.
	rest, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("ReadAll remainder: %v", err)
	}
	if string(rest) != "hello" {
		t.Errorf("remainder: got %q, want %q", string(rest), "hello")
	}
}
