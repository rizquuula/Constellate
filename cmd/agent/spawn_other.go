//go:build !linux

package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"
)

// spawnHostIfNeeded on non-Linux platforms cannot use setsid. It attempts to
// connect to an already-running host; if none is listening it falls back to
// inline spawning (the child will be in the same process group — acceptable for
// dev use; Phase 3 adds the proper daemon path per platform).
func spawnHostIfNeeded(socketPath, configPath string, log *slog.Logger) error {
	if socketResponds(socketPath) {
		return nil
	}

	log.Info("connect: session-host not responding, spawning (non-Linux fallback)", "socket", socketPath)

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate binary: %w", err)
	}

	args := []string{self, "session-host"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}
	proc, err := os.StartProcess(self, args, &os.ProcAttr{
		Dir:   ".",
		Files: []*os.File{devNull, devNull, devNull},
	})
	_ = devNull.Close()
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

func socketResponds(socketPath string) bool {
	conn, err := net.DialTimeout("unix", socketPath, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
