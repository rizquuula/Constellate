package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Hub holds the hub's configuration.
type Hub struct {
	Addr      string    `yaml:"addr"`
	PublicURL string    `yaml:"public_url"`
	DBPath    string    `yaml:"db_path"`
	DevToken  string    `yaml:"dev_token"`
	Log       LogConfig `yaml:"log"`
}

func defaultHub() Hub {
	return Hub{
		Addr:   "127.0.0.1:8080",
		DBPath: "./constellate.db",
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// LoadHub loads hub configuration from path (if non-empty) and applies
// environment variable overrides. Missing file at an explicit non-empty path
// is an error; missing file at the default path is not.
func LoadHub(path string) (Hub, error) {
	cfg := defaultHub()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return Hub{}, fmt.Errorf("config: read hub config %q: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Hub{}, fmt.Errorf("config: parse hub config %q: %w", path, err)
		}
	}

	applyHubEnv(&cfg)
	return cfg, nil
}

func applyHubEnv(cfg *Hub) {
	if v := os.Getenv("CONSTELLATE_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("CONSTELLATE_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("CONSTELLATE_DEV_TOKEN"); v != "" {
		cfg.DevToken = v
	}
	if v := os.Getenv("CONSTELLATE_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
}
