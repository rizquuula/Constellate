package pty

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	creackpty "github.com/creack/pty"

	"github.com/rizquuula/Constellate/internal/agent/app/session"
)

// Factory implements session.PTYFactory by starting real OS processes.
type Factory struct{}

// Open starts a new PTY session according to spec and returns a PTY handle.
func (Factory) Open(spec session.PTYSpec) (session.PTY, error) {
	shell := spec.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell)

	if spec.Cwd != "" {
		cmd.Dir = spec.Cwd
	}

	env := append(os.Environ(), spec.Env...)
	if !hasTERM(env) {
		env = append(env, "TERM=xterm-256color")
	}
	cmd.Env = env

	f, err := creackpty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("pty: start: %w", err)
	}

	if spec.Cols > 0 && spec.Rows > 0 {
		_ = creackpty.Setsize(f, &creackpty.Winsize{
			Cols: uint16(spec.Cols),
			Rows: uint16(spec.Rows),
		})
	}

	return &PTY{f: f, cmd: cmd}, nil
}

// hasTERM reports whether env already contains a TERM= entry.
func hasTERM(env []string) bool {
	return slices.ContainsFunc(env, func(e string) bool {
		return strings.HasPrefix(e, "TERM=")
	})
}
