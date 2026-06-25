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

	// PersistScrollback enables disk persistence of per-session scrollback
	// (default: true). When true the agent writes scrollback archives to
	// ScrollbackDir so they survive a session-host restart.
	PersistScrollback *bool `yaml:"persist_scrollback,omitempty"`

	// ScrollbackDir overrides the directory used for scrollback archives.
	// Defaults to $XDG_DATA_HOME/constellate/scrollback or
	// ~/.constellate/data/scrollback when unset.
	ScrollbackDir string `yaml:"scrollback_dir,omitempty"`

	// ScrollbackDiskCapBytes is the total-size cap (bytes) for the scrollback
	// directory. When exceeded, oldest archives are removed until under the cap.
	// Default: 64 MiB (67108864).
	ScrollbackDiskCapBytes int64 `yaml:"scrollback_disk_cap_bytes,omitempty"`
}

// defaultScrollbackDiskCap is 64 MiB.
const defaultScrollbackDiskCap int64 = 64 * 1024 * 1024

func boolPtr(b bool) *bool { return &b }

func defaultAgent() Agent {
	name, _ := os.Hostname()
	return Agent{
		Name:                   name,
		IDFile:                 expandHome("~/.constellate/agent-id"),
		CredFile:               expandHome("~/.constellate/cred"),
		ScrollbackBytes:        262144,
		PersistScrollback:      boolPtr(true),
		ScrollbackDiskCapBytes: defaultScrollbackDiskCap,
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// IsPersistScrollback reports whether scrollback persistence is enabled.
// It is true by default; explicitly setting persist_scrollback: false disables it.
func (a *Agent) IsPersistScrollback() bool {
	if a.PersistScrollback == nil {
		return true
	}
	return *a.PersistScrollback
}

// DataDir returns the data directory for durable agent files:
// $XDG_DATA_HOME/constellate when XDG_DATA_HOME is set, else ~/.constellate/data.
func DataDir() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "constellate")
	}
	return expandHome("~/.constellate/data")
}

// ScrollbackDir returns the directory for scrollback archive files, using the
// override from cfg if set, otherwise <DataDir>/scrollback.
func (a *Agent) ScrollbackDirPath() string {
	if a.ScrollbackDir != "" {
		return a.ScrollbackDir
	}
	return filepath.Join(DataDir(), "scrollback")
}

// ScrollbackCapBytes returns the disk cap for scrollback archives (bytes).
// Returns the default 64 MiB when not explicitly configured.
func (a *Agent) ScrollbackCapBytes() int64 {
	if a.ScrollbackDiskCapBytes > 0 {
		return a.ScrollbackDiskCapBytes
	}
	return defaultScrollbackDiskCap
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
