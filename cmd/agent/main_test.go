package main

import (
	"strings"
	"testing"
)

func TestRenderHostUnit(t *testing.T) {
	tests := []struct {
		name     string
		params   unitParams
		rootless bool
	}{
		{
			name:   "system with config",
			params: unitParams{ExecBin: "/usr/local/bin/constellate-agent", ConfigPath: "/etc/constellate/agent.yaml", User: "agent"},
		},
		{
			name:     "rootless without config",
			params:   unitParams{ExecBin: "/home/bob/.local/bin/constellate-agent", Rootless: true},
			rootless: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHostUnit(tc.params)

			// ExecStart must use session-host subcommand.
			wantExec := tc.params.ExecBin + " session-host"
			if tc.params.ConfigPath != "" {
				wantExec += " --config " + tc.params.ConfigPath
			}
			if !strings.Contains(got, "ExecStart="+wantExec+"\n") {
				t.Errorf("ExecStart missing:\nwant: %q\nin:\n%s", wantExec, got)
			}

			for _, want := range []string{
				"Description=Constellate session host (durable PTY owner)",
				"Type=simple",
				"Restart=on-failure",
				"RestartSec=3",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("missing directive %q in:\n%s", want, got)
				}
			}

			// connect unit must NOT have Requires= on itself.
			if strings.Contains(got, "Requires=") {
				t.Errorf("host unit must not contain Requires=, got:\n%s", got)
			}

			if tc.rootless {
				if strings.Contains(got, "User=") {
					t.Errorf("rootless unit must not contain User=, got:\n%s", got)
				}
				if !strings.Contains(got, "WantedBy=default.target") {
					t.Errorf("rootless unit missing WantedBy=default.target in:\n%s", got)
				}
			} else {
				if !strings.Contains(got, "User="+tc.params.User+"\n") {
					t.Errorf("missing User=%s in:\n%s", tc.params.User, got)
				}
				if !strings.Contains(got, "WantedBy=multi-user.target") {
					t.Errorf("missing WantedBy=multi-user.target in:\n%s", got)
				}
			}
		})
	}
}

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
		{
			name:       "rootless without config",
			params:     unitParams{ExecBin: "/home/bob/.local/bin/constellate-agent", ConfigPath: "", Rootless: true},
			wantConfig: false,
			wantExec:   "ExecStart=/home/bob/.local/bin/constellate-agent connect",
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
				"Description=Constellate agent (connect relay)",
				"Type=simple",
				"Restart=always",
				"RestartSec=3",
				"Requires=" + hostUnitServiceName,
				hostUnitServiceName, // must appear somewhere in After= line
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
