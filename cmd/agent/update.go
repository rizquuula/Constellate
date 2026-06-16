package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	platcli "github.com/rizquuula/Constellate/internal/platform/cli"
)

func cmdUpdate(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	versionFlag := fs.String("version", "", "pin a release tag (e.g. v20260615-0830)")
	checkFlag := fs.Bool("check", false, "report current vs available version and exit without updating")
	forceFlag := platcli.Bool(fs, "force", "f", false, "reinstall even if already up to date")
	noRestartFlag := fs.Bool("no-restart", false, "skip systemd service restart after update")
	rootlessFlag := platcli.Bool(fs, "rootless", "r", false, "update a rootless user install (restart via systemctl --user)")
	binFlag := fs.String("bin", "", "override target binary path (default: the running binary)")
	_ = fs.Parse(args)

	// Resolve the target binary path: --bin override, else the running binary.
	binPath := *binFlag
	if binPath == "" {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "update: locate binary: %v\n", err)
			os.Exit(1)
		}
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		binPath = exe
	}

	// Resolve the release base URL.
	const repo = "rizquuula/Constellate"
	var base string
	if *versionFlag != "" {
		base = "https://github.com/" + repo + "/releases/download/" + *versionFlag
	} else {
		base = "https://github.com/" + repo + "/releases/latest/download"
	}

	// Fetch and verify update.sh from the release using system roots (GitHub TLS).
	// A timeout bounds each request so a stalled connection can't hang the command.
	client := &http.Client{Timeout: 30 * time.Second}

	sumsData, err := httpGet(client, base+"/SHA256SUMS")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: fetch SHA256SUMS: %v\n", err)
		os.Exit(1)
	}

	sums := parseSHA256SUMS(sumsData)
	wantHex, ok := sums["update.sh"]
	if !ok {
		fmt.Fprintf(os.Stderr, "update: update.sh not found in SHA256SUMS\n")
		os.Exit(1)
	}

	scriptData, err := httpGet(client, base+"/update.sh")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: fetch update.sh: %v\n", err)
		os.Exit(1)
	}

	if !verifyChecksum(scriptData, wantHex) {
		fmt.Fprintf(os.Stderr, "update: checksum mismatch for update.sh\n")
		os.Exit(1)
	}

	// Require sh on PATH.
	shPath, err := exec.LookPath("sh")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: sh not found on PATH\n")
		fmt.Fprintf(os.Stderr, "  Run the updater directly:\n")
		fmt.Fprintf(os.Stderr, "    curl -fsSL %s/update.sh | sudo sh\n", base)
		os.Exit(1)
	}

	// Write the verified script to a temp file (0700).
	tmp, err := os.CreateTemp("", "constellate-update-*.sh")
	if err != nil {
		fmt.Fprintf(os.Stderr, "update: create temp file: %v\n", err)
		os.Exit(1)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(scriptData); err != nil {
		_ = tmp.Close()
		fmt.Fprintf(os.Stderr, "update: write temp file: %v\n", err)
		os.Exit(1)
	}
	if err := tmp.Chmod(0o700); err != nil {
		_ = tmp.Close()
		fmt.Fprintf(os.Stderr, "update: chmod temp file: %v\n", err)
		os.Exit(1)
	}
	if err := tmp.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "update: close temp file: %v\n", err)
		os.Exit(1)
	}

	// Build env: start from the process environment, then append our overrides.
	env := append(os.Environ(), flagsToEnv(*versionFlag, *checkFlag, *forceFlag, *noRestartFlag, *rootlessFlag, binPath)...)

	cmd := exec.Command(shPath, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "update: run script: %v\n", err)
		os.Exit(1)
	}
}

// parseSHA256SUMS parses a SHA256SUMS file (format: "<hash>  <filename>" per line,
// two spaces) into a map of base filename → hex hash. The filename may have a
// leading "*" (binary mode marker) which is stripped. Path components are dropped;
// only the base name is used as the key.
func parseSHA256SUMS(data []byte) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format is "<hash>  <filename>" (two spaces).
		idx := strings.Index(line, "  ")
		if idx < 0 {
			continue
		}
		hash := strings.TrimSpace(line[:idx])
		name := strings.TrimSpace(line[idx+2:])
		// Strip leading "*" (binary mode).
		name = strings.TrimPrefix(name, "*")
		// Use only the base name.
		name = filepath.Base(name)
		if hash != "" && name != "" {
			m[name] = hash
		}
	}
	return m
}

// flagsToEnv translates update flags to the KEY=VALUE env slice consumed by
// update.sh. Only non-zero/non-empty values produce entries; CONSTELLATE_BIN is
// always included when bin is non-empty.
func flagsToEnv(version string, check, force, noRestart, rootless bool, bin string) []string {
	var env []string
	if version != "" {
		env = append(env, "CONSTELLATE_VERSION="+version)
	}
	if check {
		env = append(env, "CONSTELLATE_CHECK=1")
	}
	if force {
		env = append(env, "CONSTELLATE_FORCE=1")
	}
	if noRestart {
		env = append(env, "CONSTELLATE_NO_RESTART=1")
	}
	if rootless {
		env = append(env, "CONSTELLATE_ROOTLESS=1")
	}
	if bin != "" {
		env = append(env, "CONSTELLATE_BIN="+bin)
	}
	return env
}

// verifyChecksum reports whether the SHA-256 of data matches wantHex (case-insensitive).
func verifyChecksum(data []byte, wantHex string) bool {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
	return strings.EqualFold(got, wantHex)
}

// maxUpdateBody bounds how much we read from a release asset (SHA256SUMS or
// update.sh). Both are tiny in practice; the cap stops a malicious or
// misconfigured redirect from streaming unbounded data into memory before the
// checksum can reject it — this command often runs as root.
const maxUpdateBody = 8 << 20 // 8 MiB

// httpGet fetches url with client and returns the response body. It returns an
// error for any non-200 status, a body larger than maxUpdateBody, or a
// transport failure.
func httpGet(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	// Read one extra byte so an over-cap body is detectable rather than silently truncated.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxUpdateBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxUpdateBody {
		return nil, fmt.Errorf("response from %s exceeds %d bytes", url, maxUpdateBody)
	}
	return data, nil
}
