package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TLSConfig holds TLS certificate paths for the hub.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// WebAuthnConfig holds optional WebAuthn relying-party overrides.
// When unset, values are derived from PublicURL (or defaulted to localhost in dev).
type WebAuthnConfig struct {
	RPID    string   `yaml:"rp_id"`
	Origins []string `yaml:"origins"`
}

// Hub holds the hub's configuration.
type Hub struct {
	Addr           string         `yaml:"addr"`
	PublicURL      string         `yaml:"public_url"`
	DBPath         string         `yaml:"db_path"`
	EnrollTokenTTL string         `yaml:"enroll_token_ttl"`
	TLS            TLSConfig      `yaml:"tls"`
	Log            LogConfig      `yaml:"log"`
	WebAuthn       WebAuthnConfig `yaml:"webauthn"`
}

func defaultHub() Hub {
	return Hub{
		Addr:           "127.0.0.1:8080",
		DBPath:         "./constellate.db",
		EnrollTokenTTL: "15m",
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
	if v := os.Getenv("CONSTELLATE_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
	if v := os.Getenv("CONSTELLATE_ENROLL_TOKEN_TTL"); v != "" {
		cfg.EnrollTokenTTL = v
	}
}
