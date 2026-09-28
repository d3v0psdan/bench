// Package doctor diagnoses the machine for `bench doctor` and the start
// preflight: who holds the ports Bench needs, whether Laravel Herd is
// running, and whether *.test resolves. Every problem names its culprit
// and a next step instead of surfacing a raw bind error.
package doctor

import (
	"context"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/portowner"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
)

// Check statuses.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
)

// Check actions (api.Check.Action).
const (
	ActionSetup   = "setup"
	ActionRecheck = "recheck"
)

// Fix steps shared by several checks.
var (
	// herdSteps free what Herd holds. Herd can stay installed; only one of
	// the two can serve sites at a time.
	herdSteps  = []string{"Quit Laravel Herd: click its tray icon, then Quit Herd.", "Check again."}
	setupSteps = []string{
		"Run the HTTPS and DNS setup (`bench setup` on the command line).",
		"Approve the administrator prompt.",
	}
)

// Doctor holds what the checks need to tell Bench's own processes apart
// from foreign ones.
type Doctor struct {
	Sup       *supervisor.Supervisor
	Services  func() ([]api.Service, error)
	HTTPSPort int // 0 = 443
	// RootCA is the path of Bench's local CA certificate (Caddy's pki).
	RootCA string

	// Seams for tests; nil = the real OS.
	Listener  func(port int) (portowner.Process, bool, error)
	Processes func() ([]portowner.Process, error)
	Resolve   func(ctx context.Context, host string) ([]string, error)
	ReadFile  func(path string) ([]byte, error)
	Trusted   func(*x509.Certificate) error
}

// Run executes every check. Checks never fail the call: a check that
// can't run reports itself as a warning.
func (d *Doctor) Run(ctx context.Context) []api.Check {
	run := *d // defaults fill a copy: concurrent API calls share d
	return run.run(ctx)
}

func (d *Doctor) run(ctx context.Context) []api.Check {
	d.defaults()
	herd := d.herdProcesses()
	checks := []api.Check{d.checkHerd(herd), d.checkHTTPS(), d.checkHTTP(), d.checkMail(), d.checkDNS(ctx), d.checkTrust()}
	checks = append(checks, d.checkServicePorts()...)
	if runtime.GOOS == "windows" {
		checks = append(checks, checkVCRuntime())
	}
	return checks
}

func (d *Doctor) defaults() {
	if d.HTTPSPort == 0 {
		d.HTTPSPort = 443
	}
	if d.Listener == nil {
		d.Listener = portowner.Listener
	}
	if d.Processes == nil {
		d.Processes = portowner.Processes
	}
	if d.Resolve == nil {
		d.Resolve = net.DefaultResolver.LookupHost
	}
	if d.ReadFile == nil {
		d.ReadFile = os.ReadFile
	}
	if d.Trusted == nil {
		d.Trusted = systemTrusts
	}
}

func (d *Doctor) herdProcesses() []portowner.Process {
	list, err := d.Processes()
	if err != nil {
		return nil
	}
	var out []portowner.Process
	for _, p := range list {
		if portowner.IsHerd(p) {
			out = append(out, p)
		}
	}
	return out
}

func (d *Doctor) checkHerd(herd []portowner.Process) api.Check {
	c := api.Check{ID: "herd", Title: "Laravel Herd"}
	if len(herd) > 0 {
		c.Status = Warn
		c.Detail = "Laravel Herd is running. Only one of Herd and Bench can serve sites at a time."
		c.Steps, c.Action = herdSteps, ActionRecheck
		return c
	}
	c.Status = OK
	c.Detail = "Herd is not running."
	if d.herdInstalled() {
		c.Detail = "Herd is installed but not running: no conflict. Its *.test entries also point at 127.0.0.1."
	}
	return c
}

// herdInstalled looks for Herd's footprints: its hosts-file block
// (Windows) or its dnsmasq resolver file (macOS).
func (d *Doctor) herdInstalled() bool {
	if b, err := d.ReadFile(hostsFile()); err == nil && strings.Contains(string(b), "# Herd generated Hosts") {
		return true
	}
	if runtime.GOOS == "darwin" {
		if b, err := d.ReadFile("/etc/resolver/test"); err == nil && !strings.Contains(string(b), "Bench") {
			return true
		}
	}
	return false
}

func hostsFile() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}

// ours reports whether pid is one of Bench's supervised children.
func (d *Doctor) ours(pid int) bool {
	if d.Sup == nil || pid == 0 {
		return false
	}
	for _, st := range d.Sup.Statuses() {
		if st.PID == pid {
			return true
		}
	}
	return false
}

// holder describes a foreign listener on port ("" when free or ours).
func (d *Doctor) holder(port int) (p portowner.Process, foreign bool) {
	p, ok, err := d.Listener(port)
	if err != nil || !ok || d.ours(p.PID) || p.PID == os.Getpid() {
		return p, false
	}
	return p, true
}

func (d *Doctor) checkHTTPS() api.Check {
	c := api.Check{ID: "https", Title: fmt.Sprintf("HTTPS port %d", d.HTTPSPort)}
	p, foreign := d.holder(d.HTTPSPort)
	switch {
	case !foreign:
		c.Status, c.Detail = OK, "Available to Bench (Caddy serves https://*.test on 127.0.0.1)."
	case portowner.IsHerd(p):
		c.Status = Fail
		c.Detail = fmt.Sprintf("%s is using port %d, so Bench can't serve https://*.test.", describeWithHerd(p), d.HTTPSPort)
		c.Steps, c.Action = herdSteps, ActionRecheck
	default:
		c.Status = Fail
		c.Detail = fmt.Sprintf("%s is using port %d, so Bench can't serve https://*.test.", portowner.Describe(p), d.HTTPSPort)
		c.Steps = []string{stopStep(p), "Restart Bench: choose Stop Bench in its tray menu, then open Bench again.", "Check again."}
		c.Action = ActionRecheck
	}
	return c
}

// checkHTTP: Bench serves HTTPS only, so port 80 only matters in that
// http:// links go to whoever holds it.
func (d *Doctor) checkHTTP() api.Check {
	c := api.Check{ID: "http", Title: "HTTP port 80"}
	p, foreign := d.holder(80)
	if !foreign {
		c.Status, c.Detail = OK, "Free (Bench serves HTTPS only)."
		return c
	}
	c.Status = Warn
	c.Detail = fmt.Sprintf("%s is using port 80, so http://*.test links open it instead of Bench.", describeWithHerd(p))
	if portowner.IsHerd(p) {
		c.Steps, c.Action = herdSteps, ActionRecheck
	}
	return c
}

// describeWithHerd names a port holder; Herd's processes by product, since
// their pids mean nothing to the reader.
func describeWithHerd(p portowner.Process) string {
	if portowner.IsHerd(p) && p.Name != "" {
		return "Laravel Herd (" + p.Name + ")"
	}
	return portowner.Describe(p)
}

// stopStep is the step that frees a port from a foreign program.
func stopStep(p portowner.Process) string {
	if p.Name == "" {
		return "Stop " + portowner.Describe(p) + ", or the service that runs it."
	}
	return "Stop " + p.Name + ", or the service that runs it."
}

// checkMail reports the mail catcher, or what will happen when it's enabled.
func (d *Doctor) checkMail() api.Check {
	c := api.Check{ID: "mail", Title: "Mail (SMTP)"}
	if svc, ok := d.service("mailpit"); ok {
		switch {
		case svc.State == "running" && svc.Port != 2525:
			c.Status = Warn
			why := "2525 was taken"
			if p, foreign := d.holder(2525); foreign {
				why = describeWithHerd(p) + " is using 2525"
			}
			c.Detail = fmt.Sprintf("Mail runs on port %d instead of 2525, because %s.", svc.Port, why)
			c.Steps = []string{fmt.Sprintf("Set `MAIL_PORT=%d` in each app's `.env`.", svc.Port)}
		case svc.State == "running":
			c.Status, c.Detail = OK, "Mail catcher is running on 127.0.0.1:2525."
		default:
			if p, foreign := d.holder(svc.Port); foreign {
				c.Status = Fail
				c.Detail = fmt.Sprintf("Mail is %s, and %s is using its port %d.", svc.State, describeWithHerd(p), svc.Port)
				c.Steps = []string{
					strings.TrimSuffix(stopStep(p), ".") + ". Or delete Mail and add it again to get a free port.",
					"Start Mail from the Mail tab.",
				}
				return c
			}
			c.Status = Warn
			c.Detail = fmt.Sprintf("Mail is %s, so apps can't send mail to it.", svc.State)
			c.Steps = []string{"Start Mail from the Mail tab (or run `bench services:start " + svc.Name + "`)."}
		}
		return c
	}
	p, foreign := d.holder(2525)
	if !foreign {
		c.Status, c.Detail = OK, "Port 2525 is free for Bench's mail catcher (not enabled yet)."
		return c
	}
	c.Status = Warn
	c.Detail = fmt.Sprintf("%s is using port 2525. When you add Mail, Bench picks the next free port and shows the MAIL_PORT to set.", describeWithHerd(p))
	return c
}

// checkServicePorts flags stopped instances whose port another process took.
func (d *Doctor) checkServicePorts() []api.Check {
	list, err := d.Services()
	if err != nil {
		return []api.Check{{ID: "services", Title: "Service instances", Status: Warn, Detail: "Could not list services: " + err.Error()}}
	}
	var out []api.Check
	for _, s := range list {
		if s.State == "running" || s.State == "starting" || s.Service == "mailpit" {
			continue
		}
		if p, foreign := d.holder(s.Port); foreign {
			out = append(out, api.Check{ID: "service-" + s.Name, Title: "Service " + s.Name, Status: Fail,
				Detail: fmt.Sprintf("%s is using port %d, which %s needs.", describeWithHerd(p), s.Port, s.Name),
				Steps: []string{
					strings.TrimSuffix(stopStep(p), ".") + ". Or delete " + s.Name + " and add it again on another port.",
					"Start " + s.Name + " from the Services tab.",
				}})
		}
	}
	return out
}

func (d *Doctor) service(kind string) (api.Service, bool) {
	list, err := d.Services()
	if err != nil {
		return api.Service{}, false
	}
	for _, s := range list {
		if s.Service == kind {
			return s, true
		}
	}
	return api.Service{}, false
}

func (d *Doctor) checkDNS(ctx context.Context) api.Check {
	c := api.Check{ID: "dns", Title: "*.test DNS"}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := d.Resolve(ctx, "bench-doctor-probe.test")
	if err == nil && (slices.Contains(addrs, "127.0.0.1") || slices.Contains(addrs, "::1")) {
		c.Status, c.Detail = OK, "*.test resolves to this machine."
		return c
	}
	c.Status = Warn
	c.Detail = "*.test names don't point to this machine yet, so sites won't open."
	c.Steps, c.Action = setupSteps, ActionSetup
	return c
}
