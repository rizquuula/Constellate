package terminal

import "sync"

const defaultScrollbackBytes = 256 * 1024

// Scrollback is a bounded, broadcast byte buffer with absolute offsets.
// It supports multiple concurrent readers via cancelable blocking reads.
type Scrollback struct {
	mu     sync.Mutex
	buf    []byte
	max    int
	start  int64        // absolute offset of buf[0] (oldest retained byte)
	closed bool
	wait   chan struct{} // closed to broadcast new data/close; replaced under lock
}

// NewScrollback creates a Scrollback with the given capacity.
// max <= 0 defaults to 256 KiB.
func NewScrollback(max int) *Scrollback {
	if max <= 0 {
		max = defaultScrollbackBytes
	}
	return &Scrollback{
		max:  max,
		wait: make(chan struct{}),
	}
}

// Write appends p to the buffer, evicting oldest bytes if capacity is exceeded,
// then broadcasts to all blocked readers.
func (s *Scrollback) Write(p []byte) {
	s.mu.Lock()
	if len(p) > s.max {
		// Only keep the last max bytes of the write.
		drop := len(p) - s.max
		p = p[drop:]
		s.start += int64(len(s.buf)) + int64(drop)
		s.buf = s.buf[:0]
	}
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.max {
		drop := len(s.buf) - s.max
		copy(s.buf, s.buf[drop:])
		s.buf = s.buf[:s.max]
		s.start += int64(drop)
	}
	w := s.wait
	s.wait = make(chan struct{})
	s.mu.Unlock()
	close(w)
}

// Close marks the buffer as closed and wakes all blocked readers.
func (s *Scrollback) Close() {
	s.mu.Lock()
	s.closed = true
	w := s.wait
	s.wait = make(chan struct{})
	s.mu.Unlock()
	close(w)
}

// Oldest returns the absolute offset of the oldest byte in the buffer.
// A fresh attach should call ReadFrom with this value to replay all buffered output.
func (s *Scrollback) Oldest() int64 {
	s.mu.Lock()
	v := s.start
	s.mu.Unlock()
	return v
}

// Snapshot returns a copy of all currently retained bytes under the lock.
// It is safe to call concurrently. The returned slice is a fresh allocation.
func (s *Scrollback) Snapshot() []byte {
	s.mu.Lock()
	out := append([]byte(nil), s.buf...)
	s.mu.Unlock()
	return out
}

// NewScrollbackWithData creates a Scrollback pre-seeded with data so that
// ReadFrom(Oldest(), …) replays the preloaded bytes first.
// max <= 0 defaults to 256 KiB. If data exceeds max, only the last max bytes
// are kept (matching the overflow semantics of Write).
func NewScrollbackWithData(max int, data []byte) *Scrollback {
	sb := NewScrollback(max)
	if len(data) > 0 {
		sb.Write(data)
	}
	return sb
}

// ReadFrom reads data starting at cursor, blocking until new data is available
// or done is closed. Returns (data, nextCursor, ok).
// ok=false means the buffer is closed and no more data will arrive.
// ok=false is also returned when done fires (caller should stop).
func (s *Scrollback) ReadFrom(cursor int64, done <-chan struct{}) (data []byte, next int64, ok bool) {
	s.mu.Lock()
	for {
		end := s.start + int64(len(s.buf))

		// Clamp a stale cursor that fell behind the eviction window.
		if cursor < s.start {
			cursor = s.start
		}

		if cursor < end {
			// Data is available: return a copy.
			data = append([]byte(nil), s.buf[cursor-s.start:]...)
			s.mu.Unlock()
			return data, end, true
		}

		if s.closed {
			s.mu.Unlock()
			return nil, cursor, false
		}

		// No data yet; wait for a broadcast.
		w := s.wait
		s.mu.Unlock()

		select {
		case <-w:
			// New data or close; loop to recheck under lock.
		case <-done:
			return nil, cursor, false
		}

		s.mu.Lock()
	}
}
