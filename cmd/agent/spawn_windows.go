//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"
)

// spawnHostIfNeeded on Windows cannot use setsid to detach; it spawns the
// session-host inline (best-effort — acceptable for dev use). Windows is not a
// release target, so this path exists mainly to keep the package building.
//
// configPath is passed through to the spawned host via --config so it uses the
// same config file. An empty configPath means no --config flag (default config).
func spawnHostIfNeeded(socketPath, configPath string, log *slog.Logger) error {
	if socketResponds(socketPath) {
		return nil
	}

	log.Info("connect: session-host not responding, spawning (windows)", "socket", socketPath)

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}

	args := []string{self, "session-host"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	// Open a log file for the spawned host's stdout+stderr so its output is not
	// silently discarded. The log sits next to the socket in the runtime dir.
	socketDir := filepath.Dir(socketPath)
	logPath := filepath.Join(socketDir, "host.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		log.Warn("connect: could not open host log file, using /dev/null",
			"path", logPath, "err", err)
		logFile, err = os.Open(os.DevNull)
		if err != nil {
			return fmt.Errorf("open null device: %w", err)
		}
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("open null device for stdin: %w", err)
	}

	proc, err := os.StartProcess(self, args, &os.ProcAttr{
		Dir:   ".",
		Files: []*os.File{devNull, logFile, logFile},
	})
	_ = devNull.Close()
	_ = logFile.Close()
	if err != nil {
		return fmt.Errorf("spawn session-host: %w", err)
	}
	if err := proc.Release(); err != nil {
		return fmt.Errorf("release session-host: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if socketResponds(socketPath) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("session-host did not become ready at %s within 10s", socketPath)
}

// socketResponds returns true if a connection to socketPath succeeds.
func socketResponds(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
