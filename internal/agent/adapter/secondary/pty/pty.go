package pty

import (
	"errors"
	"os"
	"os/exec"

	creackpty "github.com/creack/pty"
)

// PTY wraps a creack/pty-started process and its master file descriptor.
type PTY struct {
	f   *os.File
	cmd *exec.Cmd
}

// Read returns output from the PTY master.
func (p *PTY) Read(b []byte) (int, error) { return p.f.Read(b) }

// Write sends input to the shell via the PTY master.
func (p *PTY) Write(b []byte) (int, error) { return p.f.Write(b) }

// Resize changes the PTY window size.
func (p *PTY) Resize(cols, rows int) error {
	return creackpty.Setsize(p.f, &creackpty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
}

// Pid returns the OS PID of the shell process.
func (p *PTY) Pid() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Wait waits for the shell process to exit and returns its exit code.
func (p *PTY) Wait() (int, error) {
	err := p.cmd.Wait()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return -1, err
}

// Close terminates the PTY master file and the underlying process.
func (p *PTY) Close() error {
	_ = p.f.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return nil
}
