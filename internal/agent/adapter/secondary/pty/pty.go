package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"

	creackpty "github.com/creack/pty"
	"github.com/shirou/gopsutil/v4/process"
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

// Cwd returns the shell process's current working directory (follows cd).
func (p *PTY) Cwd() (string, error) {
	if p.cmd.Process == nil {
		return "", errors.New("pty: process not started")
	}
	pid := p.cmd.Process.Pid

	// On Linux, read /proc/<pid>/cwd directly: gopsutil's NewProcess does a
	// PidExists stat plus a /proc/<pid>/stat create-time parse we don't need,
	// and its Linux Cwd() is this same readlink. A failure here is terminal —
	// falling through to gopsutil would only repeat the call that just failed.
	if runtime.GOOS == "linux" {
		dir, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		if err != nil {
			return "", fmt.Errorf("pty: cwd of %d: %w", pid, err)
		}
		return dir, nil
	}

	// Elsewhere, go through gopsutil (darwin uses purego libproc, so this stays
	// cgo-free). process_fallback.go defines CwdWithContext, so this compiles on
	// every GOOS; unsupported platforms simply return an error.
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return "", fmt.Errorf("pty: new process %d: %w", pid, err)
	}
	dir, err := proc.Cwd()
	if err != nil {
		return "", fmt.Errorf("pty: cwd of %d: %w", pid, err)
	}
	return dir, nil
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
