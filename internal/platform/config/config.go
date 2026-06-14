package config

import (
	"os"
	"strings"
)

// LogConfig holds logging configuration shared by hub and agent.
type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// expandHome replaces a leading "~/" with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return home + path[1:]
}
