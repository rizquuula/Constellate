package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/pty"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/sysmetrics"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/vt"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	"github.com/rizquuula/Constellate/internal/agent/app/snapshot"
	platcli "github.com/rizquuula/Constellate/internal/platform/cli"
	platconfig "github.com/rizquuula/Constellate/internal/platform/config"
	"github.com/rizquuula/Constellate/internal/platform/id"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
	"github.com/rizquuula/Constellate/internal/platform/version"
	"github.com/rizquuula/Constellate/internal/transport"
)

// vtScreenFactory adapts vt.Emulator to session.Screen at the composition root.
type vtScreenFactory struct{}

func (vtScreenFactory) NewScreen(cols, rows int) session.Screen { return vt.New(cols, rows) }

func main() {
	args := os.Args[1:]

	// Top-level version/help flags, before subcommand dispatch (a leading "-"
	// would otherwise fall through to the default subcommand's flag parser).
	if len(args) > 0 {
		switch args[0] {
		case "-v", "--v", "-version", "--version":
			cmdVersion()
			return
		case "-h", "--h", "-help", "--help":
			usage()
			return
		}
	}

	// Determine subcommand; default is "connect".
	sub := "connect"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "version":
		cmdVersion()
	case "status":
		cmdStatus(args)
	case "connect":
		cmdConnect(args)
	case "enroll":
		cmdEnroll(args)
	case "reset":
		cmdReset(args)
	case "install":
		cmdInstall(args)
	case "update":
		cmdUpdate(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`constellate-agent — dials home to a Constellate hub and owns local PTYs.

Usage:
  constellate-agent [command] [flags]

Commands:
  connect   Connect to the hub and serve sessions (default)
  enroll    Enroll this machine with the hub (--hub, --token)
  status    Show local enrollment status
  reset     Remove local enrollment (id + credential)
  install   Install + start a systemd service (Linux; requires enrollment)
  update    Download + install the latest agent release, then restart the service
  version   Print version and protocol info

Flags:
  -v, --version   Print version and protocol info
  -h, --help      Show this help

Run "constellate-agent <command> -h" for command-specific flags.`)
}

func cmdVersion() {
	fmt.Printf("constellate-agent %s (commit %s, proto %d)\n",
		version.Version, version.Commit, transport.ProtocolVersion)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := platcli.ConfigFlag(fs)
	_ = platcli.String(fs, "log-level", "l", "", "log level override (debug/info/warn/error)")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: load config: %v\n", err)
		os.Exit(1)
	}

	machineID := "—"
	enrolled := "no"
	if cfg.IDFile != "" {
		if data, err := os.ReadFile(cfg.IDFile); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				machineID = trimmed
				enrolled = "yes"
			}
		}
	}

	fmt.Printf("enrolled:   %s\n", enrolled)
	fmt.Printf("machine id: %s\n", machineID)
	fmt.Printf("name:       %s\n", cfg.Name)
	fmt.Printf("hub:        %s\n", cfg.HubURL)
	fmt.Println("(live connectivity requires a running agent daemon — not checked here)")
}

func cmdConnect(args []string) {
	fs := flag.NewFlagSet("connect", flag.ExitOnError)
	configPath := platcli.ConfigFlag(fs)
	logLevel := platcli.String(fs, "log-level", "l", "", "log level override (debug/info/warn/error)")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: load config: %v\n", err)
		os.Exit(1)
	}

	level := cfg.Log.Level
	if *logLevel != "" {
		level = *logLevel
	}
	log := platlog.New(level, cfg.Log.Format)

	if cfg.HubURL == "" {
		log.Error("connect: hub_url is required")
		os.Exit(1)
	}

	// Load the agent credential. Enrollment is required — both the machine ID
	// and the private key must exist. Direct the operator to enroll if missing.
	if cfg.CredFile == "" || cfg.IDFile == "" {
		log.Error("not enrolled: run `constellate-agent enroll --hub <url> --token <token>`")
		os.Exit(1)
	}

	machineID, err := readMachineID(cfg.IDFile)
	if err != nil {
		log.Error("not enrolled: machine id file missing or empty — run `constellate-agent enroll --hub <url> --token <token>`", "err", err)
		os.Exit(1)
	}

	agentKey, err := loadPrivateKey(cfg.CredFile)
	if err != nil {
		log.Error("not enrolled: credential file missing or unreadable — run `constellate-agent enroll --hub <url> --token <token>`", "err", err)
		os.Exit(1)
	}

	// Build an HTTP client that trusts the hub's CA certificate (if configured).
	httpClient, err := buildHTTPClient(cfg.HubCA)
	if err != nil {
		log.Error("connect: build http client", "err", err)
		os.Exit(1)
	}

	instanceID := id.New()

	mgr := session.NewManager(pty.Factory{}, cfg.ScrollbackBytes, log)

	client := hubclient.New(hubclient.Config{
		HubURL:            cfg.HubURL,
		AgentKey:          agentKey,
		HTTPClient:        httpClient,
		MachineID:         machineID,
		InstanceID:        instanceID,
		Name:              cfg.Name,
		HeartbeatInterval: 5 * time.Second,
		Log:               log,
		Sessions:          mgr,
		Metrics:           sysmetrics.Collector{},
	})

	prod := snapshot.New(mgr, client, snapshot.DefaultInterval, log)
	client.SetSnapshotToggle(prod)
	mgr.SetScreenFactory(vtScreenFactory{})
	mgr.SetNotifier(client)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("connecting", "hub", cfg.HubURL, "machineID", machineID)

	go func() { _ = prod.Run(ctx) }()

	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("connect: run error", "err", err)
		os.Exit(1)
	}

	mgr.Shutdown()
}

// readMachineID reads the machine ID from path. Returns an error if the file
// is missing or empty — callers should direct the operator to enroll first.
func readMachineID(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("id file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", fmt.Errorf("id file %s is empty", path)
	}
	return trimmed, nil
}

// buildHTTPClient returns an *http.Client whose TLS roots include the PEM file
// at caPath (in addition to system roots). When caPath is empty, the default
// http.Client (nil) is returned, which uses system roots.
func buildHTTPClient(caPath string) (*http.Client, error) {
	if caPath == "" {
		return nil, nil
	}
	caPEM, err := os.ReadFile(caPath)
	if err != nil {
		return nil, fmt.Errorf("read hub_ca %q: %w", caPath, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("hub_ca %q: no valid PEM certificates found", caPath)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
	}, nil
}

// loadPrivateKey reads a PEM PKCS8 Ed25519 private key from path.
func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", path)
	}
	raw, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKCS8 key from %s: %w", path, err)
	}
	key, ok := raw.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key in %s is not an Ed25519 key", path)
	}
	return key, nil
}

// writePrivateKey encodes priv as PEM PKCS8 and writes it to path (mode 0600).
func writePrivateKey(path string, priv ed25519.PrivateKey) error {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("marshal PKCS8 key: %w", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	buf := pem.EncodeToMemory(block)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cred dir: %w", err)
	}
	return os.WriteFile(path, buf, 0o600)
}

// hubHTTPBase derives the HTTP base URL from cfg.HubURL when --hub is not set.
// It converts ws→http and wss→https and strips a trailing /ws/agent suffix.
func hubHTTPBase(hubURL string) string {
	u := hubURL
	u = strings.TrimSuffix(u, "/ws/agent")
	if strings.HasPrefix(u, "wss://") {
		u = "https://" + u[len("wss://"):]
	} else if strings.HasPrefix(u, "ws://") {
		u = "http://" + u[len("ws://"):]
	}
	return u
}

func cmdEnroll(args []string) {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	configPath := platcli.ConfigFlag(fs)
	hubFlag := platcli.String(fs, "hub", "H", "", "hub HTTP base URL (e.g. http://localhost:8080)")
	tokenFlag := platcli.String(fs, "token", "t", "", "enrollment token (required)")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll: load config: %v\n", err)
		os.Exit(1)
	}

	if *tokenFlag == "" {
		fmt.Fprintln(os.Stderr, "enroll: --token is required")
		os.Exit(1)
	}

	base := *hubFlag
	if base == "" {
		base = hubHTTPBase(cfg.HubURL)
	}
	if base == "" {
		fmt.Fprintln(os.Stderr, "enroll: hub URL required (--hub or hub_url in config)")
		os.Exit(1)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll: generate keypair: %v\n", err)
		os.Exit(1)
	}

	type enrollReq struct {
		Token     string `json:"token"`
		PublicKey string `json:"publicKey"`
		Name      string `json:"name"`
		OS        string `json:"os"`
		Arch      string `json:"arch"`
	}
	reqBody := enrollReq{
		Token:     *tokenFlag,
		PublicKey: base64.StdEncoding.EncodeToString(pub),
		Name:      cfg.Name,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	body, _ := json.Marshal(reqBody)

	httpClient, err := buildHTTPClient(cfg.HubCA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll: build http client: %v\n", err)
		os.Exit(1)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	req, err := http.NewRequest(http.MethodPost, base+"/api/enroll", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll: create request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll: POST /api/enroll: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintf(os.Stderr, "enroll: server error %d: %s\n", resp.StatusCode, strings.TrimSpace(string(respBody)))
		os.Exit(1)
	}

	var enrollResp struct {
		MachineID string `json:"machineID"`
	}
	if err := json.Unmarshal(respBody, &enrollResp); err != nil {
		fmt.Fprintf(os.Stderr, "enroll: parse response: %v\n", err)
		os.Exit(1)
	}

	// Write machineID.
	if err := os.MkdirAll(filepath.Dir(cfg.IDFile), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "enroll: create id dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(cfg.IDFile, []byte(enrollResp.MachineID), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "enroll: write id file: %v\n", err)
		os.Exit(1)
	}

	// Write private key.
	if err := writePrivateKey(cfg.CredFile, priv); err != nil {
		fmt.Fprintf(os.Stderr, "enroll: write cred file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("enrolled: machineID=%s\n", enrollResp.MachineID)
}

func cmdReset(args []string) {
	fs := flag.NewFlagSet("reset", flag.ExitOnError)
	configPath := platcli.ConfigFlag(fs)
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reset: load config: %v\n", err)
		os.Exit(1)
	}

	for _, path := range []string{cfg.IDFile, cfg.CredFile} {
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "reset: remove %s: %v\n", path, err)
			os.Exit(1)
		}
	}
	fmt.Println("reset done")
}

// unitServiceName is the systemd unit installed by `install`.
const unitServiceName = "constellate-agent.service"

// systemUnitPath is where a system-wide unit is written (requires root).
const systemUnitPath = "/etc/systemd/system/" + unitServiceName

// userUnitPath resolves the per-user unit path (~/.config/systemd/user/<unit>).
func userUnitPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", unitServiceName), nil
}

// unitParams holds the values rendered into the systemd unit template.
type unitParams struct {
	ExecBin    string // absolute path to the agent binary
	ConfigPath string // absolute config path; empty omits the --config flag
	User       string // system user the service runs as
	Rootless   bool   // when true, render a systemctl --user unit (no User= line, WantedBy=default.target)
}

// renderUnit renders a systemd service unit from p. It is pure (no I/O) so it
// can be unit-tested without root or a running systemd.
func renderUnit(p unitParams) string {
	execLine := p.ExecBin + " connect"
	if p.ConfigPath != "" {
		execLine += " --config " + p.ConfigPath
	}
	if p.Rootless {
		return fmt.Sprintf(`[Unit]
Description=Constellate agent

[Service]
Type=simple
ExecStart=%s
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
`, execLine)
	}
	return fmt.Sprintf(`[Unit]
Description=Constellate agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
ExecStart=%s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, p.User, execLine)
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	configPath := platcli.String(fs, "config", "c", "", "path to config file (passed through to the service's connect command)")
	userFlag := fs.String("user", "", "system user to run the service as (default: $SUDO_USER, else current user)")
	noStart := fs.Bool("no-start", false, "write + reload the unit but do not enable/start it")
	rootless := platcli.Bool(fs, "rootless", "r", false, "install a rootless systemd *user* service (~/.config/systemd/user, systemctl --user) — no root required")
	_ = fs.Parse(args)

	// systemd is Linux-only; macOS (launchd) / Windows stay documented-manual.
	if runtime.GOOS != "linux" {
		fmt.Fprintf(os.Stderr, "install: only supported on Linux (systemd); see docs/usage.agent.md for %s\n", runtime.GOOS)
		os.Exit(1)
	}

	// Writing to /etc/systemd/system and running systemctl require root.
	// In rootless mode the unit goes to ~/.config/systemd/user — no root needed.
	if !*rootless && os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "install: must run as root — re-run with sudo (or use --rootless for a user service)")
		os.Exit(1)
	}

	cfg, err := platconfig.LoadAgent(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "install: load config: %v\n", err)
		os.Exit(1)
	}

	// Enrollment gate: refuse to install a service that can't connect. Mirrors
	// the credential checks in cmdConnect.
	if _, err := readMachineID(cfg.IDFile); err != nil {
		fmt.Fprintln(os.Stderr, "install: not enrolled — run `constellate-agent enroll --hub <url> --token <token>` first")
		os.Exit(1)
	}
	if _, err := loadPrivateKey(cfg.CredFile); err != nil {
		fmt.Fprintln(os.Stderr, "install: not enrolled — credential missing; run `constellate-agent enroll --hub <url> --token <token>` first")
		os.Exit(1)
	}

	// Resolve the absolute binary path so ExecStart survives a $PATH/cwd change.
	execBin, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "install: locate binary: %v\n", err)
		os.Exit(1)
	}
	if resolved, err := filepath.EvalSymlinks(execBin); err == nil {
		execBin = resolved
	}

	// Pass the same --config through to the service, absolutized.
	absConfig := ""
	if *configPath != "" {
		if abs, err := filepath.Abs(*configPath); err == nil {
			absConfig = abs
		} else {
			absConfig = *configPath
		}
	}

	var unitContent string
	var writePath string
	var runAs string

	if *rootless {
		// Rootless: user unit — no User= line, WantedBy=default.target.
		// --user <name> is ignored in this mode.
		unitContent = renderUnit(unitParams{ExecBin: execBin, ConfigPath: absConfig, Rootless: true})
		p, err := userUnitPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "install: resolve user unit path: %v\n", err)
			os.Exit(1)
		}
		writePath = p
		if err := os.MkdirAll(filepath.Dir(writePath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "install: create systemd user dir: %v\n", err)
			os.Exit(1)
		}
	} else {
		// System mode: requires root, writes to /etc/systemd/system/.
		runAs = resolveServiceUser(*userFlag)
		if runAs == "" {
			fmt.Fprintln(os.Stderr, "install: could not determine a user to run as — pass --user")
			os.Exit(1)
		}
		unitContent = renderUnit(unitParams{ExecBin: execBin, ConfigPath: absConfig, User: runAs})
		writePath = systemUnitPath
	}

	if err := os.WriteFile(writePath, []byte(unitContent), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "install: write %s: %v\n", writePath, err)
		os.Exit(1)
	}

	if *rootless {
		fmt.Printf("wrote %s (rootless user service)\n", writePath)
	} else {
		fmt.Printf("wrote %s (User=%s)\n", writePath, runAs)
	}

	if err := runSystemctl(*rootless, "daemon-reload"); err != nil {
		fmt.Fprintf(os.Stderr, "install: systemctl daemon-reload: %v\n", err)
		os.Exit(1)
	}

	if *noStart {
		if *rootless {
			fmt.Printf("installed (not started). Start with: systemctl --user enable --now %s\n", unitServiceName)
		} else {
			fmt.Printf("installed (not started). Start with: systemctl enable --now %s\n", unitServiceName)
		}
		return
	}

	if err := runSystemctl(*rootless, "enable", "--now", unitServiceName); err != nil {
		fmt.Fprintf(os.Stderr, "install: systemctl enable --now: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("started %s\n", unitServiceName)
	if *rootless {
		fmt.Printf("  systemctl --user status %s\n", unitServiceName)
		fmt.Printf("  journalctl --user -u %s -f\n", unitServiceName)

		// Linger hint: a --user service stops when the user logs out unless
		// lingering is enabled. Show the command to enable it.
		currentUser := ""
		if u, err := user.Current(); err == nil {
			currentUser = u.Username
		}
		fmt.Println()
		fmt.Println("Note: systemd --user services stop when you log out.")
		fmt.Println("To keep the agent running after logout, enable lingering:")
		if currentUser != "" {
			fmt.Printf("  loginctl enable-linger %s\n", currentUser)
		} else {
			fmt.Printf("  loginctl enable-linger <your-username>\n")
		}
		fmt.Println("(This command may require sudo or polkit authorisation.)")
	} else {
		fmt.Printf("  systemctl status %s\n", unitServiceName)
		fmt.Printf("  journalctl -u %s -f\n", unitServiceName)
	}
}

// resolveServiceUser picks the user the service runs as: the --user override,
// else the real user behind sudo ($SUDO_USER), else the current user.
func resolveServiceUser(override string) string {
	if override != "" {
		return override
	}
	if sudoUser := os.Getenv("SUDO_USER"); sudoUser != "" && sudoUser != "root" {
		return sudoUser
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// runSystemctl runs `systemctl <args...>`, streaming output to the operator.
// When rootless is true, --user is prepended so the user manager is targeted.
func runSystemctl(rootless bool, args ...string) error {
	if rootless {
		args = append([]string{"--user"}, args...)
	}
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
