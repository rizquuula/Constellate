package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/httpapi"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsbrowser"
	"github.com/rizquuula/Constellate/internal/hub/adapter/primary/wsagent"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/agentlink"
	memstore "github.com/rizquuula/Constellate/internal/hub/adapter/secondary/memory"
	"github.com/rizquuula/Constellate/internal/hub/adapter/secondary/sqlite"
	totpadapter "github.com/rizquuula/Constellate/internal/hub/adapter/secondary/totp"
	waadapter "github.com/rizquuula/Constellate/internal/hub/adapter/secondary/webauthn"
	"github.com/rizquuula/Constellate/internal/hub/app/attach"
	auditapp "github.com/rizquuula/Constellate/internal/hub/app/audit"
	authapp "github.com/rizquuula/Constellate/internal/hub/app/auth"
	"github.com/rizquuula/Constellate/internal/hub/app/dashboard"
	"github.com/rizquuula/Constellate/internal/hub/app/enroll"
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
var _ attach.AuditSink = (*auditapp.UseCase)(nil)
var _ sessions.AuditSink = (*auditapp.UseCase)(nil)
var _ wsagent.AgentAuthenticator = (*enroll.UseCase)(nil)
var _ wsagent.SessionEvents = (*sessions.UseCase)(nil)
var _ httpapi.EnrollService = (*enroll.UseCase)(nil)
var _ httpapi.AuthService = (*authapp.UseCase)(nil)
var _ authapp.WebAuthn = (*waadapter.Provider)(nil)
var _ httpapi.DashboardService = (*dashboard.UseCase)(nil)

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
	case "enroll-token":
		cmdEnrollToken(args)
	case "machines":
		cmdMachines(args)
	case "revoke":
		cmdRevoke(args)
	case "operator":
		cmdOperator(args)
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

// resolveWebAuthnConfig derives RPID and Origins for the WebAuthn provider.
// Priority: explicit config block > derived from PublicURL > localhost dev defaults.
func resolveWebAuthnConfig(cfg platconfig.Hub, log *slog.Logger) (rpID string, origins []string) {
	if cfg.WebAuthn.RPID != "" {
		return cfg.WebAuthn.RPID, cfg.WebAuthn.Origins
	}
	if cfg.PublicURL != "" {
		u, err := url.Parse(cfg.PublicURL)
		if err == nil && u.Hostname() != "" {
			return u.Hostname(), []string{cfg.PublicURL}
		}
		log.Warn("webauthn: could not parse public_url, falling back to localhost", "url", cfg.PublicURL)
	}
	return "localhost", []string{"http://localhost:8080", "http://localhost:5173"}
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
	auditStore := sqlite.NewAuditStore(db)
	enrollTokenStore := sqlite.NewEnrollTokenStore(db)
	credStore := sqlite.NewCredentialStore(db)
	links := agentlink.NewRegistry()
	gateway := agentlink.NewGateway(links)
	reg := registry.New(machineStore, links, registry.SystemClock{}, log)
	auditUC := auditapp.New(auditStore, auditapp.SystemClock{}, log)
	sessionsUC := sessions.New(sessStore, gateway, sessions.SystemClock{}, id.New, log, auditUC)
	projectsUC := projects.New(projStore, projects.SystemClock{}, id.New, log)
	attachUC := attach.New(sessStore, gateway, log, auditUC)
	overviewUC := overview.New(gateway, log)

	enrollTTL, err := time.ParseDuration(cfg.EnrollTokenTTL)
	if err != nil {
		log.Error("serve: invalid enroll_token_ttl", "value", cfg.EnrollTokenTTL, "err", err)
		os.Exit(1)
	}
	enrollUC := enroll.New(enrollTokenStore, credStore, machineStore, auditUC, enroll.SystemClock{}, id.New, enrollTTL, log)

	operatorStore := sqlite.NewOperatorStore(db, id.New)
	sessionStore := sqlite.NewOperatorSessionStore(db)
	totpVerifier := totpadapter.New()
	secureCookies := strings.HasPrefix(cfg.PublicURL, "https")

	// Resolve WebAuthn RPID and origins.
	waRPID, waOrigins := resolveWebAuthnConfig(cfg, log)
	var waProvider authapp.WebAuthn
	var waChallenge authapp.ChallengeStore
	if p, err := waadapter.New(waRPID, waOrigins); err != nil {
		log.Warn("webauthn: init failed, passkey login unavailable", "err", err)
	} else {
		waProvider = p
		waChallenge = memstore.NewChallengeStore()
	}

	authUC := authapp.New(operatorStore, sessionStore, totpVerifier, auditUC, authapp.SystemClock{}, id.New, 24*time.Hour, log, waProvider, waChallenge)

	dashboardUC := dashboard.New(machineStore, links, sessStore, projStore, auditStore, log)
	endpoint := wsagent.NewEndpoint(reg, links, sessionsUC, overviewUC, enrollUC, log)
	termHandler := wsbrowser.NewTerminalHandler(attachUC, log)
	overviewHandler := wsbrowser.NewOverviewHandler(overviewUC, log)
	srv := httpapi.NewServer(cfg.Addr, reg, sessionsUC, projectsUC, enrollUC, endpoint, termHandler, overviewHandler, authUC, dashboardUC, secureCookies, log)

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if cfg.TLS.Cert != "" && cfg.TLS.Key != "" {
			log.Info("hub listening (TLS)", "addr", srv.Addr())
			if err := srv.StartTLS(cfg.TLS.Cert, cfg.TLS.Key); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("serve: server error (TLS)", "err", err)
				stop()
			}
		} else {
			log.Info("hub listening", "addr", srv.Addr())
			if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Error("serve: server error", "err", err)
				stop()
			}
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

func cmdEnrollToken(args []string) {
	fs := flag.NewFlagSet("enroll-token", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	ttlStr := fs.String("ttl", "", "token TTL (default from config, e.g. 15m)")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll-token: load config: %v\n", err)
		os.Exit(1)
	}

	if *ttlStr != "" {
		cfg.EnrollTokenTTL = *ttlStr
	}
	ttl, err := time.ParseDuration(cfg.EnrollTokenTTL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll-token: invalid ttl %q: %v\n", cfg.EnrollTokenTTL, err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll-token: open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "enroll-token: migrate: %v\n", err)
		os.Exit(1)
	}

	log := platlog.New("error", "text")
	uc := enroll.New(
		sqlite.NewEnrollTokenStore(db),
		sqlite.NewCredentialStore(db),
		sqlite.NewMachineStore(db),
		auditapp.New(sqlite.NewAuditStore(db), auditapp.SystemClock{}, log),
		enroll.SystemClock{},
		id.New,
		ttl,
		log,
	)

	token, err := uc.MintToken(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "enroll-token: mint: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(token)
}

func cmdMachines(args []string) {
	fs := flag.NewFlagSet("machines", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "machines: load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "machines: open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "machines: migrate: %v\n", err)
		os.Exit(1)
	}

	log := platlog.New("error", "text")
	uc := enroll.New(
		sqlite.NewEnrollTokenStore(db),
		sqlite.NewCredentialStore(db),
		sqlite.NewMachineStore(db),
		auditapp.New(sqlite.NewAuditStore(db), auditapp.SystemClock{}, log),
		enroll.SystemClock{},
		id.New,
		15*time.Minute,
		log,
	)

	machines, err := uc.List(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "machines: list: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%-26s  %-20s  %-8s  %-10s  %-10s  %s\n",
		"ID", "NAME", "OS", "ENROLLED", "LAST_SEEN", "REVOKED")
	for _, m := range machines {
		revoked := ""
		if m.Revoked() {
			revoked = "yes"
		}
		fmt.Printf("%-26s  %-20s  %-8s  %-10d  %-10d  %s\n",
			m.ID(), m.Name(), m.OS(), m.EnrolledAt(), m.LastSeenAt(), revoked)
	}
}

func cmdRevoke(args []string) {
	fs := flag.NewFlagSet("revoke", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	_ = fs.Parse(args)

	remaining := fs.Args()
	if len(remaining) == 0 {
		fmt.Fprintln(os.Stderr, "revoke: machine ID required")
		os.Exit(1)
	}
	machineID := remaining[0]

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "revoke: load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "revoke: open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "revoke: migrate: %v\n", err)
		os.Exit(1)
	}

	log := platlog.New("error", "text")
	uc := enroll.New(
		sqlite.NewEnrollTokenStore(db),
		sqlite.NewCredentialStore(db),
		sqlite.NewMachineStore(db),
		auditapp.New(sqlite.NewAuditStore(db), auditapp.SystemClock{}, log),
		enroll.SystemClock{},
		id.New,
		15*time.Minute,
		log,
	)

	if err := uc.Revoke(ctx, machineID); err != nil {
		fmt.Fprintf(os.Stderr, "revoke: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("machine %s revoked\n", machineID)
}

func cmdOperator(args []string) {
	sub := "add"
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "add":
		cmdOperatorAdd(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown operator subcommand %q\n", sub)
		os.Exit(1)
	}
}

func cmdOperatorAdd(args []string) {
	fs := flag.NewFlagSet("operator add", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	issuer := fs.String("issuer", "Constellate", "TOTP issuer name")
	account := fs.String("account", "operator", "TOTP account name")
	_ = fs.Parse(args)

	cfg, err := platconfig.LoadHub(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "operator add: load config: %v\n", err)
		os.Exit(1)
	}

	db, err := sqlite.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "operator add: open db: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	ctx := context.Background()
	if err := sqlite.Migrate(ctx, db); err != nil {
		fmt.Fprintf(os.Stderr, "operator add: migrate: %v\n", err)
		os.Exit(1)
	}

	log := platlog.New("error", "text")
	auditUC := auditapp.New(sqlite.NewAuditStore(db), auditapp.SystemClock{}, log)
	operatorStore := sqlite.NewOperatorStore(db, id.New)
	sessionStore := sqlite.NewOperatorSessionStore(db)
	totpVerifier := totpadapter.New()
	authUC := authapp.New(operatorStore, sessionStore, totpVerifier, auditUC, authapp.SystemClock{}, id.New, 24*time.Hour, log, nil, nil)

	secret, uri, codes, err := authUC.BootstrapTOTP(ctx, *issuer, *account)
	if err != nil {
		fmt.Fprintf(os.Stderr, "operator add: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("TOTP secret:", secret)
	fmt.Println("Scan this URI in your authenticator app:")
	fmt.Println(uri)
	fmt.Println("\nRecovery codes (store these safely):")
	for _, c := range codes {
		fmt.Println(" ", c)
	}
}
