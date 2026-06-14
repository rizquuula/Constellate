package transport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// AttachHeader is the first line sent on an attach connection.
// After the header the stream carries raw PTY bytes.
type AttachHeader struct {
	Type      MessageType `json:"type"`
	SessionID string      `json:"sessionID"`
}

// NewAttachHeader constructs an AttachHeader with the Type field pre-set.
func NewAttachHeader(sessionID string) AttachHeader {
	return AttachHeader{
		Type:      TypeAttach,
		SessionID: sessionID,
	}
}

// ReadAttachHeader reads the first newline-terminated JSON line from r,
// decodes it into an AttachHeader, and returns the same buffered reader so
// the caller can continue reading the raw byte stream without losing bytes
// that may have been read ahead into the buffer.
func ReadAttachHeader(r io.Reader) (AttachHeader, *bufio.Reader, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return AttachHeader{}, br, err
	}
	var hdr AttachHeader
	if err := json.Unmarshal(line, &hdr); err != nil {
		return AttachHeader{}, br, fmt.Errorf("transport: unmarshal attach header: %w", err)
	}
	return hdr, br, nil
}
