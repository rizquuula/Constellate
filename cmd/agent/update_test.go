package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestParseSHA256SUMS(t *testing.T) {
	// A realistic SHA256SUMS sample mirroring what the release workflow generates.
	input := `3a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b  constellate-agent-v0.1.1-linux-amd64.tar.gz
4b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c  constellate-agent-v0.1.1-linux-arm64.tar.gz
5c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d  constellate-agent-v0.1.1-darwin-amd64.tar.gz
6d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e  constellate-agent-v0.1.1-darwin-arm64.tar.gz
7e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f  constellate-hub-v0.1.1-linux-amd64.tar.gz
8f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a  update.sh
`
	m := parseSHA256SUMS([]byte(input))

	cases := []struct {
		key  string
		want string
	}{
		{"constellate-agent-v0.1.1-linux-amd64.tar.gz", "3a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b"},
		{"constellate-agent-v0.1.1-linux-arm64.tar.gz", "4b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c"},
		{"constellate-agent-v0.1.1-darwin-amd64.tar.gz", "5c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d"},
		{"constellate-agent-v0.1.1-darwin-arm64.tar.gz", "6d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e"},
		{"constellate-hub-v0.1.1-linux-amd64.tar.gz", "7e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f"},
		{"update.sh", "8f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a"},
	}

	for _, c := range cases {
		got, ok := m[c.key]
		if !ok {
			t.Errorf("parseSHA256SUMS: key %q not found in map", c.key)
			continue
		}
		if got != c.want {
			t.Errorf("parseSHA256SUMS: key %q: got %q, want %q", c.key, got, c.want)
		}
	}

	if len(m) != 6 {
		t.Errorf("parseSHA256SUMS: got %d entries, want 6", len(m))
	}
}

func TestParseSHA256SUMS_BinaryMarker(t *testing.T) {
	// Some sha256sum implementations emit a leading "*" in binary mode.
	input := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab  *update.sh\n"
	m := parseSHA256SUMS([]byte(input))
	got, ok := m["update.sh"]
	if !ok {
		t.Fatal("parseSHA256SUMS: key 'update.sh' not found when name has leading '*'")
	}
	if got != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab" {
		t.Errorf("parseSHA256SUMS: unexpected hash %q", got)
	}
}

func TestParseSHA256SUMS_EmptyAndComments(t *testing.T) {
	input := "\n# comment\n\nabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890ab  update.sh\n"
	m := parseSHA256SUMS([]byte(input))
	if len(m) != 1 {
		t.Errorf("parseSHA256SUMS: got %d entries, want 1", len(m))
	}
	if _, ok := m["update.sh"]; !ok {
		t.Error("parseSHA256SUMS: update.sh not found")
	}
}

func TestFlagsToEnv(t *testing.T) {
	cases := []struct {
		name      string
		version   string
		check     bool
		force     bool
		noRestart bool
		rootless  bool
		bin       string
		want      []string
	}{
		{
			name: "defaults (only bin set)",
			bin:  "/usr/local/bin/constellate-agent",
			want: []string{"CONSTELLATE_BIN=/usr/local/bin/constellate-agent"},
		},
		{
			name:  "check flag",
			check: true,
			bin:   "/usr/local/bin/constellate-agent",
			want:  []string{"CONSTELLATE_CHECK=1", "CONSTELLATE_BIN=/usr/local/bin/constellate-agent"},
		},
		{
			name:    "version pin",
			version: "v20260615-0830",
			bin:     "/usr/local/bin/constellate-agent",
			want:    []string{"CONSTELLATE_VERSION=v20260615-0830", "CONSTELLATE_BIN=/usr/local/bin/constellate-agent"},
		},
		{
			name:      "no-restart",
			noRestart: true,
			bin:       "/usr/local/bin/constellate-agent",
			want:      []string{"CONSTELLATE_NO_RESTART=1", "CONSTELLATE_BIN=/usr/local/bin/constellate-agent"},
		},
		{
			name:     "rootless",
			rootless: true,
			bin:      "/home/bob/.local/bin/constellate-agent",
			want:     []string{"CONSTELLATE_ROOTLESS=1", "CONSTELLATE_BIN=/home/bob/.local/bin/constellate-agent"},
		},
		{
			name:      "all flags",
			version:   "v20260615-1200",
			check:     true,
			force:     true,
			noRestart: true,
			rootless:  true,
			bin:       "/usr/bin/constellate-agent",
			want: []string{
				"CONSTELLATE_VERSION=v20260615-1200",
				"CONSTELLATE_CHECK=1",
				"CONSTELLATE_FORCE=1",
				"CONSTELLATE_NO_RESTART=1",
				"CONSTELLATE_ROOTLESS=1",
				"CONSTELLATE_BIN=/usr/bin/constellate-agent",
			},
		},
		{
			name: "no bin, no flags",
			want: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := flagsToEnv(c.version, c.check, c.force, c.noRestart, c.rootless, c.bin)
			if len(got) != len(c.want) {
				t.Fatalf("flagsToEnv: got %v, want %v", got, c.want)
			}
			for i, v := range c.want {
				if got[i] != v {
					t.Errorf("flagsToEnv[%d]: got %q, want %q", i, got[i], v)
				}
			}
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello, world\n")
	sum := sha256.Sum256(data)
	goodHex := hex.EncodeToString(sum[:])

	if !verifyChecksum(data, goodHex) {
		t.Error("verifyChecksum: expected true for matching hash")
	}

	// Leading/trailing whitespace must not match — the hash is compared verbatim.
	if verifyChecksum(data, "  "+goodHex) {
		t.Error("verifyChecksum: expected false for hash with leading spaces")
	}

	// Upper-cased hex should match (case-insensitive).
	upperHex := hex.EncodeToString(sum[:])
	for i := range []byte(upperHex) {
		if upperHex[i] >= 'a' && upperHex[i] <= 'f' {
			upperHex = upperHex[:i] + string(upperHex[i]-32) + upperHex[i+1:]
		}
	}
	if !verifyChecksum(data, upperHex) {
		t.Error("verifyChecksum: expected true for uppercase matching hash")
	}

	// Wrong hash.
	if verifyChecksum(data, "0000000000000000000000000000000000000000000000000000000000000000") {
		t.Error("verifyChecksum: expected false for wrong hash")
	}

	// Different data.
	if verifyChecksum([]byte("other"), goodHex) {
		t.Error("verifyChecksum: expected false for different data")
	}
}
