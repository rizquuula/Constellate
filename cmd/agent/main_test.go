package main

import (
	"strings"
	"testing"
)

func TestRenderUnit(t *testing.T) {
	tests := []struct {
		name        string
		params      unitParams
		wantConfig  bool
		wantExec    string
		wantUserStr string
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
			if !strings.Contains(got, tc.wantUserStr+"\n") {
				t.Errorf("missing %q in:\n%s", tc.wantUserStr, got)
			}

			// Static directives that must always be present.
			for _, want := range []string{
				"Description=Constellate agent",
				"After=network-online.target",
				"Wants=network-online.target",
				"Type=simple",
				"Restart=always",
				"RestartSec=3",
				"WantedBy=multi-user.target",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("missing static directive %q in:\n%s", want, got)
				}
			}
		})
	}
}
