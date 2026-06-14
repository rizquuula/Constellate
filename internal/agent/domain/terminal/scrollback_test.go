package terminal

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestScrollbackWriteThenReadFromOldest(t *testing.T) {
	sb := NewScrollback(1024)
	sb.Write([]byte("hello"))
	sb.Write([]byte(" world"))

	done := make(chan struct{})
	data, next, ok := sb.ReadFrom(sb.Oldest(), done)
	if !ok {
		t.Fatal("ReadFrom returned ok=false unexpectedly")
	}
	if !bytes.Equal(data, []byte("hello world")) {
		t.Errorf("got %q, want %q", data, "hello world")
	}
	if next != int64(len("hello world")) {
		t.Errorf("next=%d, want %d", next, len("hello world"))
	}
}

func TestScrollbackMultipleWritesAccumulate(t *testing.T) {
	sb := NewScrollback(4096)
	for i := 0; i < 5; i++ {
		sb.Write([]byte("x"))
	}

	done := make(chan struct{})
	data, _, ok := sb.ReadFrom(0, done)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(data) != 5 {
		t.Errorf("len=%d, want 5", len(data))
	}
}

func TestScrollbackOverflowDropsOldest(t *testing.T) {
	max := 10
	sb := NewScrollback(max)
	sb.Write([]byte("AAAAAAAAAA")) // exactly 10 bytes
	sb.Write([]byte("BBB"))        // causes 3 bytes of A to be dropped

	if sb.Oldest() != 3 {
		t.Errorf("Oldest=%d, want 3", sb.Oldest())
	}

	done := make(chan struct{})
	data, _, ok := sb.ReadFrom(sb.Oldest(), done)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !bytes.Equal(data, []byte("AAAAAAABBB")) {
		t.Errorf("got %q, want %q", data, "AAAAAAABBB")
	}
}

func TestScrollbackSingleOverflowWrite(t *testing.T) {
	max := 5
	sb := NewScrollback(max)
	sb.Write([]byte("0123456789")) // 10 bytes into a 5-byte buffer

	if sb.Oldest() != 5 {
		t.Errorf("Oldest=%d, want 5", sb.Oldest())
	}

	done := make(chan struct{})
	data, _, ok := sb.ReadFrom(sb.Oldest(), done)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !bytes.Equal(data, []byte("56789")) {
		t.Errorf("got %q, want %q", data, "56789")
	}
}

func TestScrollbackStaleCursorClamps(t *testing.T) {
	max := 5
	sb := NewScrollback(max)
	sb.Write([]byte("0123456789")) // Oldest() == 5 after this

	done := make(chan struct{})
	// Cursor 0 is behind Oldest (5); should clamp and return what's available.
	data, next, ok := sb.ReadFrom(0, done)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !bytes.Equal(data, []byte("56789")) {
		t.Errorf("got %q, want %q", data, "56789")
	}
	if next != 10 {
		t.Errorf("next=%d, want 10", next)
	}
}

func TestScrollbackCloseUnblocksReader(t *testing.T) {
	sb := NewScrollback(1024)
	done := make(chan struct{})

	result := make(chan bool, 1)
	go func() {
		_, _, ok := sb.ReadFrom(0, done)
		result <- ok
	}()

	time.Sleep(20 * time.Millisecond) // let reader block
	sb.Close()

	select {
	case ok := <-result:
		if ok {
			t.Error("expected ok=false after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock ReadFrom")
	}
}

func TestScrollbackDoneCancelsBlockedReader(t *testing.T) {
	sb := NewScrollback(1024)
	done := make(chan struct{})

	result := make(chan bool, 1)
	go func() {
		_, _, ok := sb.ReadFrom(0, done)
		result <- ok
	}()

	time.Sleep(20 * time.Millisecond) // let reader block
	close(done)

	select {
	case ok := <-result:
		if ok {
			t.Error("expected ok=false after done closed")
		}
	case <-time.After(time.Second):
		t.Fatal("closing done did not unblock ReadFrom")
	}
}

func TestScrollbackConcurrentWriterReader(t *testing.T) {
	sb := NewScrollback(4096)
	done := make(chan struct{})
	defer close(done)

	const writes = 100
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < writes; i++ {
			sb.Write([]byte("X"))
		}
		sb.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		cursor := sb.Oldest()
		for {
			data, next, ok := sb.ReadFrom(cursor, done)
			_ = data
			cursor = next
			if !ok {
				return
			}
		}
	}()

	wg.Wait()
}
