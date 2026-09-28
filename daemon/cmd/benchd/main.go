// benchd is the Bench daemon: it owns the site registry and serves the
// localhost REST + WebSocket API that the GUI and CLI consume.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/caddy"
	"github.com/d3v0psdan/bench/daemon/internal/dnsstub"
	"github.com/d3v0psdan/bench/daemon/internal/doctor"
	"github.com/d3v0psdan/bench/daemon/internal/elevate"
	"github.com/d3v0psdan/bench/daemon/internal/mail"
	"github.com/d3v0psdan/bench/daemon/internal/newapp"
	"github.com/d3v0psdan/bench/daemon/internal/paths"
	"github.com/d3v0psdan/bench/daemon/internal/phppool"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/server"
	"github.com/d3v0psdan/bench/daemon/internal/services"
	"github.com/d3v0psdan/bench/daemon/internal/setup"
	"github.com/d3v0psdan/bench/daemon/internal/sitedelete"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
	"github.com/d3v0psdan/bench/daemon/internal/token"
	"github.com/d3v0psdan/bench/daemon/internal/version"
)

func main() {
	port := flag.Int("port", api.DefaultPort, "listen port (binds 127.0.0.1 only)")
	flag.Parse()
	os.Exit(realMain(*port))
}

// realMain sets up logging first so that startup failures reach the log
// file: when spawned detached (bench start, GUI), stderr goes to the null
// device and the log is the only place an error can surface.
func realMain(port int) int {
	root, err := paths.EnsureRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchd:", err)
		return 1
	}
	// Don't pin the spawner's working directory for the daemon's lifetime:
	// it blocks deleting/unmounting whatever directory the user ran
	// `bench start` (or the GUI) from.
	if err := os.Chdir(root); err != nil {
		fmt.Fprintln(os.Stderr, "benchd:", err)
		return 1
	}

	logFile, err := paths.LogFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchd:", err)
		return 1
	}
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "benchd: opening log file:", err)
		return 1
	}
	defer f.Close()
	log := slog.New(slog.NewTextHandler(io.MultiWriter(os.Stderr, f), nil))

	if err := run(port, log); err != nil {
		log.Error("benchd failed", "err", err)
		return 1
	}
	return 0
}

// runSetup performs the privileged one-time setup with a single
// elevation prompt: trust Caddy's local CA and route .test to the stub.
func runSetup(ctx context.Context, cdy *caddy.Caddy, dns, trust bool) ([]string, error) {
	var args, msgs []string
	if trust {
		// The CA is provisioned when Caddy first loads the pki app, so
		// make sure it exists before pointing the helper at it.
		if err := cdy.Ensure(ctx); err != nil {
			return nil, fmt.Errorf("starting caddy to provision its CA: %w", err)
		}
		ca := cdy.RootCAPath()
		if err := waitForFile(ctx, ca, 15*time.Second); err != nil {
			return nil, fmt.Errorf("caddy CA certificate never appeared at %s: %w", ca, err)
		}
		args = append(args, "-ca", ca)
		msgs = append(msgs, "local certificate authority trusted system-wide")
	}
	if dns {
		args = append(args, "-dns", dnsstub.ListenAddr())
		msgs = append(msgs, "*.test now resolves to 127.0.0.1")
	}
	if len(args) == 0 {
		return nil, nil
	}
	if err := elevate.RunHelper(ctx, append([]string{"setup"}, args...)...); err != nil {
		return nil, err
	}
	return msgs, nil
}

func waitForFile(ctx context.Context, path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func run(port int, log *slog.Logger) error {
	tokenFile, err := paths.TokenFile()
	if err != nil {
		return err
	}
	tok, err := token.LoadOrCreate(tokenFile)
	if err != nil {
		return err
	}

	dbFile, err := paths.DBFile()
	if err != nil {
		return err
	}
	reg, err := registry.Open(dbFile)
	if err != nil {
		return err
	}
	defer reg.Close()

	root, err := paths.Root()
	if err != nil {
		return err
	}
	manifests, err := binman.LoadEmbedded()
	if err != nil {
		return err
	}
	binaries := &binman.Manager{Root: root, Log: log, Manifests: manifests}

	sup := supervisor.New(log)
	defer sup.StopAll() // children die before the registry closes (LIFO)
	cdy := &caddy.Caddy{Root: root, Bin: binaries, Sup: sup, Log: log}
	pools := &phppool.Pools{Root: root, Bin: binaries, Sup: sup, Log: log}
	siteMgr := &sites.Manager{Reg: reg, Router: cdy, Pools: pools, Log: log}
	if err := siteMgr.NormalizePHP(func(v string) (string, bool) { return binaries.Channel("php", v) }); err != nil {
		log.Warn("storing PHP versions by channel failed; older full-version values stay", "err", err)
	}
	svcMgr := &services.Manager{Root: root, Reg: reg, Bin: binaries, Sup: sup, Log: log}

	// Embedded *.test resolver. Failure is non-fatal: sites still work
	// via the hosts-file fallback documented in README.md (DNS fallback).
	stub := &dnsstub.Stub{Log: log}
	if err := stub.Start(); err != nil {
		log.Error("dns stub failed to start", "err", err)
	} else {
		defer stub.Stop()
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("listening on port %d: %w", port, err)
	}

	addrFile, err := paths.AddrFile()
	if err != nil {
		return err
	}
	if err := os.WriteFile(addrFile, []byte(ln.Addr().String()+"\n"), 0o644); err != nil {
		return fmt.Errorf("writing addr file: %w", err)
	}
	defer os.Remove(addrFile)

	// Shutdown is triggered by SIGINT/SIGTERM or POST /api/shutdown.
	shutdown := make(chan struct{})
	var once sync.Once
	requestShutdown := func() { once.Do(func() { close(shutdown) }) }

	srv := &server.Server{
		Version:    version.Version,
		Token:      tok,
		StartedAt:  time.Now(),
		Log:        log,
		OnShutdown: requestShutdown,
		Binaries:   binaries,
		RemovePHP:  pools.Remove,
		PHPInfo:    pools.Info,
		Sites:      siteMgr,
		Services:   svcMgr,
		Doctor:     &doctor.Doctor{Sup: sup, Services: svcMgr.List, RootCA: cdy.RootCAPath()},
		Mail:       mail.Proxy(mail.FromServices(svcMgr.List)),
		MailSave:   mail.SaveAttachment(mail.FromServices(svcMgr.List), paths.Downloads),
	}
	taskReg := &tasks.Registry{OnChange: func(list []api.Task) {
		srv.Publish(api.Event{Type: "tasks", Tasks: list})
	}}
	srv.Tasks = taskReg
	svcMgr.Tasks = taskReg
	srv.NewApp = &newapp.Creator{Root: root, Bin: binaries, Pools: pools, Sites: siteMgr, Services: svcMgr, Tasks: taskReg, Log: log}
	srv.SiteDelete = &sitedelete.Deleter{Root: root, Sites: siteMgr, Services: svcMgr, Tasks: taskReg, Mail: mail.FromServices(svcMgr.List), Log: log}
	binaries.OnProgress = func(p binman.Progress) {
		srv.Publish(api.Event{Type: "download", Download: &p})
		taskReg.Progress(p)
	}
	siteMgr.OnChange = func(list []api.Site) {
		srv.Publish(api.Event{Type: "sites", Sites: list})
	}
	srv.Setup = func(ctx context.Context, dns, trust bool) ([]string, error) {
		return runSetup(ctx, cdy, dns, trust)
	}
	srv.Teardown = func(ctx context.Context, dns, trust bool) ([]string, error) {
		return setup.Teardown(ctx, root, dns, trust)
	}
	svcMgr.OnChange = func(list []api.Service) {
		srv.Publish(api.Event{Type: "services", Services: list})
	}
	sup.OnEvent = func(st supervisor.Status) {
		srv.Publish(api.Event{Type: "process", Process: &st})
		if services.IsProcName(st.Name) {
			svcMgr.Notify()
		}
	}

	// Restore sites from the registry (exit criterion: daemon restart
	// brings every site back with no manual steps). Async: a first-boot
	// Caddy download must not block the API from coming up.
	go func() {
		// Generous: downloads now stop when their caller's ctx ends, and a
		// first-boot Caddy download on a slow link must not be cut short.
		rctx, rcancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer rcancel()
		if _, err := siteMgr.Apply(rctx); err != nil {
			log.Error("restoring sites failed", "err", err)
		}
	}()
	// Live mail events for the GUI's inbox, whenever Mailpit runs.
	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go mail.Relay(relayCtx, mail.FromServices(svcMgr.List), srv.Publish, log)
	// Autostart service instances (exit criterion: they come back after
	// bench stop && bench start). Async for the same reason as sites.
	// Each start is bounded by the manager and cancelled by svcMgr.Close.
	go svcMgr.StartAutostart()
	httpSrv := &http.Server{Handler: srv.Handler()}

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpSrv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	log.Info("benchd started", "version", version.Version, "addr", ln.Addr().String(), "pid", os.Getpid())

	select {
	case s := <-sig:
		log.Info("signal received, shutting down", "signal", s.String())
	case <-shutdown:
		log.Info("shutdown requested via api")
	case err := <-serveErr:
		return fmt.Errorf("api server: %w", err)
	}

	// Children first, while the API still answers: `bench stop` polls
	// until the API is gone, so it must not vanish while databases are
	// still flushing (a quick `bench start` would race their ports).
	svcMgr.Close() // cancel in-flight creates/clones before their processes go
	sup.StopAll()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		log.Warn("graceful shutdown incomplete", "err", err)
	}
	log.Info("benchd stopped")
	return nil
}
