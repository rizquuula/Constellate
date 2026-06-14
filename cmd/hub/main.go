package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsbrowser"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsagent"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	"github.com/rizquuula/Constellate/internal/hub/app/attach"
	"github.com/rizquuula/Constellate/internal/hub/app/overview"
	"github.com/rizquuula/Constellate/internal/hub/app/projects"
	"github.com/rizquuula/Constellate/internal/hub/app/registry"
	"github.com/rizquuula/Constellate/internal/hub/app/sessions"
	platconfig "github.com/rizquuula/Constellate/internal/platform/config"
	platlog "github.com/rizquuula/Constellate/internal/platform/log"
	"github.com/rizquuula/Constellate/internal/platform/id"
	"github.com/rizquuula/Constellate/internal/platform/version"
	"github.com/rizquuula/Constellate/internal/transport"
)

// Compile-time interface assertions.
var _ sessions.AgentGateway = (*agentlink.Gateway)(nil)
var _ attach.AgentGateway = (*agentlink.Gateway)(nil)
var _ overview.SnapshotControl = (*agentlink.Gateway)(nil)
var _ projects.ProjectStore = (*sqlite.ProjectStore)(nil)
var _ sessions.SessionStore = (*sqlite.SessionStore)(nil)
var _ httpapi.ProjectService = (*projects.UseCase)(nil)
var _ httpapi.SessionService = (*sessions.UseCase)(nil)

func main() {
	args := os.Args[1:]

	// Determine subcommand.
	sub := "serve"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "version":
		cmdVersion()
	case "migrate":
		cmdMigrate(args)
	case "serve":
		cmdServe(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		os.Exit(1)
	}
}

func cmdVersion() {
	fmt.Printf("constellate-hub %s (commit %s, proto %d)\n",
		version.Version, version.Commit, transport.ProtocolVersion)
}

func cmdMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("migrations applied")
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	logLevel := fs.String("log-level", "", "log level override (debug/info/warn/error)")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "serve: load config: %v\n", err)
		os.Exit(1)
	}

	level := cfg.Log.Level
	if *logLevel != "" {
		level = *logLevel
	}
	log := platlog.New(level, cfg.Log.Format)

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		log.Error("serve: open db", "err", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		log.Error("serve: migrate", "err", err)
		os.Exit(1)
	}

	machineStore := sqlite.NewMachineStore(db)
	sessStore := sqlite.NewSessionStore(db)
	projStore := sqlite.NewProjectStore(db)
	links := agentlink.NewRegistry()
	gateway := agentlink.NewGateway(links)
	reg := registry.New(machineStore, links, registry.SystemClock{}, log)
	sessionsUC := sessions.New(sessStore, gateway, sessions.SystemClock{}, id.New, log)
	projectsUC := projects.New(projStore, projects.SystemClock{}, id.New, log)
	attachUC := attach.New(sessStore, gateway, log)
	overviewUC := overview.New(gateway, log)

	endpoint := wsagent.NewEndpoint(reg, links, sessionsUC, overviewUC, cfg.DevToken, log)
	termHandler := wsbrowser.NewTerminalHandler(attachUC, log)
	overviewHandler := wsbrowser.NewOverviewHandler(overviewUC, log)
	srv := httpapi.NewServer(cfg.Addr, reg, sessionsUC, projectsUC, endpoint, termHandler, overviewHandler, log)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info("hub listening", "addr", srv.Addr())
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve: server error", "err", err)
			stop()
		}
	}()

	<-sigCtx.Done()
	log.Info("shutting down")

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()

	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("serve: shutdown error", "err", err)
		os.Exit(1)
	}
}
