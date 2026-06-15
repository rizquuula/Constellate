package pty

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/app/session"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	cases := []struct {
		in, want string
	}{
		{"~", home},
		{"~/proj", filepath.Join(home, "proj")},
		{"/abs/path", "/abs/path"},
		{"relative", "relative"},
		{"", ""},
		{"~user", "~user"}, // not our "~" form — left as-is
	}
	for _, c := range cases {
		if got := expandHome(c.in); got != c.want {
			t.Errorf("expandHome(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFactoryCwdTilde verifies the agent starts the shell in the home directory
// when the UI sends "~" as the cwd (regression: a literal "~" chdir fails with
// ENOENT before the shell can expand it).
func TestFactoryCwdTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}

	p, err := Factory{}.Open(session.PTYSpec{
		Shell: "/bin/sh",
		Cwd:   "~",
		Cols:  80,
		Rows:  24,
	})
	if err != nil {
		t.Fatalf("Open with cwd ~: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	if _, err := p.Write([]byte("pwd\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		_ = p.(*PTY).f.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _ := p.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if strings.Contains(out.String(), home) {
			break
		}
	}

	if !strings.Contains(out.String(), home) {
		t.Errorf("expected pwd output to contain home %q, got: %q", home, out.String())
	}
}

func TestFactoryIntegration(t *testing.T) {
	p, err := Factory{}.Open(session.PTYSpec{
		Shell: "/bin/sh",
		Cols:  80,
		Rows:  24,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	if p.Pid() <= 0 {
		t.Errorf("Pid: got %d, expected positive", p.Pid())
	}

	if _, err := p.Write([]byte("echo constellate\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var out bytes.Buffer
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		_ = p.(*PTY).f.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, _ := p.Read(buf)
		if n > 0 {
			out.Write(buf[:n])
		}
		if strings.Contains(out.String(), "constellate") {
			break
		}
	}

	if !strings.Contains(out.String(), "constellate") {
		t.Errorf("expected %q in PTY output, got: %q", "constellate", out.String())
	}

	if err := p.Resize(100, 30); err != nil {
		t.Errorf("Resize: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
