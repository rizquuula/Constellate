package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Agent holds the agent's configuration.
type Agent struct {
	HubURL          string    `yaml:"hub_url"`
	Name            string    `yaml:"name"`
	IDFile          string    `yaml:"id_file"`
	CredFile        string    `yaml:"cred_file"`
	HubCA           string    `yaml:"hub_ca"`
	DefaultShell    string    `yaml:"default_shell"`
	ScrollbackBytes int       `yaml:"scrollback_bytes"`
	Log             LogConfig `yaml:"log"`
	// RuntimeDir is the directory used for the session-host socket and other
	// runtime files. Defaults to $XDG_RUNTIME_DIR/constellate if set, else
	// ~/.constellate/run. The socket is at <runtime_dir>/host.sock.
	RuntimeDir string `yaml:"runtime_dir"`
}

func defaultAgent() Agent {
	name, _ := os.Hostname()
	return Agent{
		Name:            name,
		IDFile:          expandHome("~/.constellate/agent-id"),
		CredFile:        expandHome("~/.constellate/cred"),
		ScrollbackBytes: 262144,
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// SocketPath returns the path for the session-host Unix domain socket derived
// from RuntimeDir. If RuntimeDir is empty, it uses $XDG_RUNTIME_DIR/constellate
// when available, otherwise ~/.constellate/run.
func (a *Agent) SocketPath() string {
	dir := a.RuntimeDir
	if dir == "" {
		if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
			dir = filepath.Join(xdg, "constellate")
		} else {
			dir = expandHome("~/.constellate/run")
		}
	}
	return filepath.Join(dir, "host.sock")
}

// LoadAgent loads agent configuration from path (if non-empty) and applies
// environment variable overrides. Missing file at an explicit non-empty path
// is an error; missing file at the default path is not.
func LoadAgent(path string) (Agent, error) {
	cfg := defaultAgent()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Agent{}, fmt.Errorf("config: read agent config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Agent{}, fmt.Errorf("config: parse agent config %q: %w", path, err)
		}
	}

	applyAgentEnv(&cfg)
	cfg.IDFile = expandHome(cfg.IDFile)
	cfg.CredFile = expandHome(cfg.CredFile)
	if cfg.HubCA != "" {
		cfg.HubCA = expandHome(cfg.HubCA)
	}
	return cfg, nil
}

func applyAgentEnv(cfg *Agent) {
	if v := os.Getenv("CONSTELLATE_HUB_URL"); v != "" {
		cfg.HubURL = v
	}
	if v := os.Getenv("CONSTELLATE_NAME"); v != "" {
		cfg.Name = v
	}
	if v := os.Getenv("CONSTELLATE_ID_FILE"); v != "" {
		cfg.IDFile = v
	}
	if v := os.Getenv("CONSTELLATE_CRED_FILE"); v != "" {
		cfg.CredFile = v
	}
	if v := os.Getenv("CONSTELLATE_HUB_CA"); v != "" {
		cfg.HubCA = v
	}
	if v := os.Getenv("CONSTELLATE_RUNTIME_DIR"); v != "" {
		cfg.RuntimeDir = v
	}
}
