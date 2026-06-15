package pty

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		// The UI sends "~" (and "~/sub") to mean the home directory; the shell
		// would expand it, but exec.Cmd chdirs to Dir verbatim *before* the shell
		// runs, so a literal "~" fails with ENOENT. Expand it here.
		dir := expandHome(spec.Cwd)
		if _, err := os.Stat(dir); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("pty: stat cwd: %w", err)
			}
			// Directory is missing: either create it on request, or surface a
			// distinct error so the hub/UI can offer to create it (rather than a
			// cryptic fork/exec ENOENT from the shell launch).
			if !spec.CreateDir {
				return nil, session.ErrCwdNotFound
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("pty: create cwd: %w", err)
			}
		}
		cmd.Dir = dir
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

// expandHome replaces a leading "~" (alone or as "~/...") with the user's home
// directory. Any other path is returned unchanged. If the home directory cannot
// be determined, the original path is returned so the caller's own error
// handling applies.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// hasTERM reports whether env already contains a TERM= entry.
func hasTERM(env []string) bool {
	return slices.ContainsFunc(env, func(e string) bool {
		return strings.HasPrefix(e, "TERM=")
	})
}
