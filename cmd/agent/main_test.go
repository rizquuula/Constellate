package main

import (
	"strings"
	"testing"
)

func TestRenderUnit(t *testing.T) {
	tests := []struct {
		name       string
		params     unitParams
		wantConfig bool
		wantExec   string
		// system-mode expectations
		wantUserStr string // non-empty means assert User=<str> is present
		// rootless-mode expectations
		rootless bool
	}{
		{
			name:        "with config",
			params:      unitParams{ExecBin: "/usr/local/bin/constellate-agent", ConfigPath: "/home/rizq/.constellate/agent.yaml", User: "rizq"},
			wantConfig:  true,
			wantExec:    "ExecStart=/usr/local/bin/constellate-agent connect --config /home/rizq/.constellate/agent.yaml",
			wantUserStr: "User=rizq",
		},
		{
			name:        "without config",
			params:      unitParams{ExecBin: "/usr/local/bin/constellate-agent", ConfigPath: "", User: "alice"},
			wantConfig:  false,
			wantExec:    "ExecStart=/usr/local/bin/constellate-agent connect",
			wantUserStr: "User=alice",
		},
		{
			name:       "rootless with config",
			params:     unitParams{ExecBin: "/home/bob/.local/bin/constellate-agent", ConfigPath: "/home/bob/.constellate/agent.yaml", Rootless: true},
			wantConfig: true,
			wantExec:   "ExecStart=/home/bob/.local/bin/constellate-agent connect --config /home/bob/.constellate/agent.yaml",
			rootless:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderUnit(tc.params)

			if !strings.Contains(got, tc.wantExec+"\n") {
				t.Errorf("ExecStart line missing or wrong:\nwant: %q\nin:\n%s", tc.wantExec, got)
			}
			if hasConfig := strings.Contains(got, "--config"); hasConfig != tc.wantConfig {
				t.Errorf("--config present=%v, want %v", hasConfig, tc.wantConfig)
			}

			// Directives always present regardless of mode.
			for _, want := range []string{
				"Description=Constellate agent",
				"Type=simple",
				"Restart=always",
				"RestartSec=3",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("missing static directive %q in:\n%s", want, got)
				}
			}

			if tc.rootless {
				// Rootless-specific assertions.
				if strings.Contains(got, "User=") {
					t.Errorf("rootless unit must not contain User= line, got:\n%s", got)
				}
				if strings.Contains(got, "network-online.target") {
					t.Errorf("rootless unit must not contain network-online.target, got:\n%s", got)
				}
				if !strings.Contains(got, "WantedBy=default.target") {
					t.Errorf("rootless unit missing WantedBy=default.target in:\n%s", got)
				}
			} else {
				// System-mode assertions.
				if tc.wantUserStr != "" && !strings.Contains(got, tc.wantUserStr+"\n") {
					t.Errorf("missing %q in:\n%s", tc.wantUserStr, got)
				}
				for _, want := range []string{
					"After=network-online.target",
					"Wants=network-online.target",
					"WantedBy=multi-user.target",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("missing system directive %q in:\n%s", want, got)
					}
				}
			}
		})
	}
}
