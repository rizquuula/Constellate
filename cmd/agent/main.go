package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rizquuula/Constellate/internal/agent/adapter/primary/hubclient"
	"github.com/rizquuula/Constellate/internal/agent/adapter/secondary/pty"
	"github.com/rizquuula/Constellate/internal/agent/app/session"
	platconfig "github.com/rizquuula/Constellate/internal/platform/config"
	"github.com/rizquuula/Constellate/internal/platform/id"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
	"github.com/rizquuula/Constellate/internal/platform/version"
	"github.com/rizquuula/Constellate/internal/transport"
)

func main() {
	args := os.Args[1:]

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
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		os.Exit(1)
	}
}

func cmdVersion() {
	fmt.Printf("constellate-agent %s (commit %s, proto %d)\n",
		version.Version, version.Commit, transport.ProtocolVersion)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	_ = fs.String("log-level", "", "log level override (debug/info/warn/error)")
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
	configPath := fs.String("config", "", "path to config file")
	logLevel := fs.String("log-level", "", "log level override (debug/info/warn/error)")
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

	if cfg.IDFile == "" {
		log.Error("connect: id_file path is required but empty")
		os.Exit(1)
	}

	machineID, err := ensureMachineID(cfg.IDFile)
	if err != nil {
		log.Error("connect: ensure machine id", "err", err)
		os.Exit(1)
	}

	if cfg.HubURL == "" {
		log.Error("connect: hub_url is required")
		os.Exit(1)
	}

	instanceID := id.New()

	mgr := session.NewManager(pty.Factory{}, cfg.ScrollbackBytes, log)

	client := hubclient.New(hubclient.Config{
		HubURL:            cfg.HubURL,
		DevToken:          cfg.DevToken,
		MachineID:         machineID,
		InstanceID:        instanceID,
		Name:              cfg.Name,
		HeartbeatInterval: 5 * time.Second,
		Log:               log,
		Sessions:          mgr,
	})
	mgr.SetNotifier(client)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("connecting", "hub", cfg.HubURL, "machineID", machineID)

	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("connect: run error", "err", err)
		os.Exit(1)
	}

	mgr.Shutdown()
}

// ensureMachineID reads the machine ID from path if the file exists; otherwise
// it generates a new ID and writes it to path (creating parent directories as
// needed). Returns an error if path is empty.
func ensureMachineID(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("id file path is empty")
	}

	if data, err := os.ReadFile(path); err == nil {
		trimmed := strings.TrimSpace(string(data))
		if trimmed != "" {
			return trimmed, nil
		}
	}

	newID := id.New()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create id file directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(newID), 0o600); err != nil {
		return "", fmt.Errorf("write id file: %w", err)
	}
	return newID, nil
}
