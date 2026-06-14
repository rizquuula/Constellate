package transport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Decoder reads NDJSON frames from a stream.
type Decoder struct {
	r *bufio.Reader
}

// NewDecoder creates a Decoder reading from r.
func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: bufio.NewReader(r)}
}

// Next reads the next newline-terminated JSON line and returns a Frame.
// It returns io.EOF when the stream ends cleanly.
func (d *Decoder) Next() (Frame, error) {
	line, err := d.r.ReadBytes('\n')
	if err != nil {
		if len(line) == 0 {
			return Frame{}, err
		}
		// partial line before EOF — try to decode anyway, but return the error too
	}

	var hdr typeHeader
	if jsonErr := json.Unmarshal(line, &hdr); jsonErr != nil {
		return Frame{}, fmt.Errorf("transport: decode type header: %w", jsonErr)
	}

	raw := make(json.RawMessage, len(line))
	copy(raw, line)

	return Frame{Type: hdr.Type, Raw: raw}, err
}

// Encoder writes NDJSON frames to a stream. Safe for concurrent use.
type Encoder struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// NewEncoder creates an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: bufio.NewWriter(w)}
}

// Encode marshals msg to JSON, appends a newline, and flushes.
func (e *Encoder) Encode(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("transport: marshal: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if _, err := e.w.Write(data); err != nil {
		return fmt.Errorf("transport: write: %w", err)
	}
	if err := e.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("transport: write newline: %w", err)
	}
	if err := e.w.Flush(); err != nil {
		return fmt.Errorf("transport: flush: %w", err)
	}
	return nil
}

// Unmarshal decodes a Frame's raw bytes into a value of type T.
func Unmarshal[T any](f Frame) (T, error) {
	var out T
	if err := json.Unmarshal(f.Raw, &out); err != nil {
		return out, fmt.Errorf("transport: unmarshal %T: %w", out, err)
	}
	return out, nil
}
