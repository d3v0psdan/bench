// Package services manages native service instances (MySQL, MariaDB,
// PostgreSQL, Valkey, Meilisearch, RustFS, Mailpit): install the binary
// through binman, initialize a data dir, run the server under the
// supervisor, clone instances with their data, and restore autostart
// instances when benchd boots. Instances live in the registry's services
// table; runtime state comes from the supervisor.
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
	"github.com/d3v0psdan/bench/daemon/internal/desktop"
	"github.com/d3v0psdan/bench/daemon/internal/registry"
	"github.com/d3v0psdan/bench/daemon/internal/sites"
	"github.com/d3v0psdan/bench/daemon/internal/supervisor"
	"github.com/d3v0psdan/bench/daemon/internal/tasks"
)

// createTimeout bounds a whole create/clone, startTimeout a start (which
// may reinstall a deleted build). The work is detached from the request:
// a CLI Ctrl-C or a closed GUI must not leave a half-made instance.
const (
	createTimeout = 30 * time.Minute
	startTimeout  = 10 * time.Minute
)

// closeGrace bounds how long Close waits for in-flight work. A download
// in progress can't be interrupted (binman shares it with other callers),
// so shutdown must not wait for it to finish.
const closeGrace = 15 * time.Second

// markerFile records that a data dir finished initializing, for which
// service kind and channel ("mysql 8.4"). Only a create ever initializes
// a dir; start never wipes one.
const markerFile = ".bench-initialized"

// creatingSuffix marks a data dir whose create is in progress: the file
// <data dir>.bench-creating exists from before init until the marker is
// written, so a crashed create's leftovers can be told apart from data
// Bench didn't make (which is never deleted).
const creatingSuffix = ".bench-creating"

// maxNameLen keeps instance names short enough for Unix socket paths
// (<home>/run/svc-<name>/mysqld.sock must stay under ~104 bytes).
const maxNameLen = 32

// ErrBusy reports an instance already mid-operation (create, clone, ...).
var ErrBusy = errors.New("service instance is busy")

// InputError is a request the caller can fix (unknown service, bad name,
// taken port); the API maps it to 400.
type InputError struct{ msg string }

func (e InputError) Error() string { return e.msg }

func inputErrorf(format string, args ...any) error {
	return InputError{msg: fmt.Sprintf(format, args...)}
}

// ProcName is the supervisor process name for an instance.
func ProcName(name string) string { return "svc-" + name }

// IsProcName reports whether a supervisor process belongs to an instance.
func IsProcName(proc string) bool { return strings.HasPrefix(proc, "svc-") }

// Manager owns every service instance's lifecycle.
type Manager struct {
	Root string // bench home
	Reg  *registry.Registry
	Bin  *binman.Manager
	Sup  *supervisor.Supervisor
	Log  *slog.Logger
	// OnChange, if set, receives the fresh instance list whenever an
	// instance changes phase or process state (feeds the "services" WS
	// event). Must not block.
	OnChange func([]api.Service)
	// Tasks, if set, tracks creates and clones for the Activity view and
	// lets a create be cancelled while it downloads.
	Tasks *tasks.Registry

	// mu guards the fields below; it is never held across supervisor or
	// install calls (supervisor events call back into List).
	mu      sync.Mutex
	phase   map[string]string           // name → lifecycle phase in progress
	pending map[string]registry.Service // creates/clones not yet in the registry
	lastErr map[string]string           // name → last start failure
	opsCtx  context.Context             // parent of all detached work; cancelled by Close
	stopOps context.CancelFunc
	closed  bool

	ops     sync.WaitGroup // detached create/clone/start work in flight
	allocMu sync.Mutex     // port allocation is atomic across concurrent creates
	pubMu   sync.Mutex     // publishes land in order: a stale list never arrives last
}

// config is the JSON in a service row's config column. Secrets are
// generated per instance (fillSecrets): a loopback-only server is still
// reachable from any web page through DNS rebinding, so nothing may run
// with a credential that's public or empty.
type config struct {
	Ports      map[string]int `json:"ports,omitempty"`
	MasterKey  string         `json:"master_key,omitempty"`  // Meilisearch
	UIPassword string         `json:"ui_password,omitempty"` // Mailpit web UI and API (user "bench")
	AccessKey  string         `json:"access_key,omitempty"`  // RustFS
	SecretKey  string         `json:"secret_key,omitempty"`  // RustFS
}

// fillSecrets generates any credential this kind needs that cfg lacks,
// reporting whether it changed anything (older instances get theirs on
// their next start).
func fillSecrets(kind string, cfg *config) (bool, error) {
	var missing []*string
	switch kind {
	case "meilisearch":
		missing = []*string{&cfg.MasterKey}
	case "mailpit":
		missing = []*string{&cfg.UIPassword}
	case "rustfs":
		missing = []*string{&cfg.AccessKey, &cfg.SecretKey}
	}
	changed := false
	for _, field := range missing {
		if *field != "" {
			continue
		}
		key, err := randomKey()
		if err != nil {
			return false, err
		}
		*field = key
		changed = true
	}
	if kind == "rustfs" && changed && len(cfg.AccessKey) > 20 {
		cfg.AccessKey = "bench" + cfg.AccessKey[:15] // S3 access keys are short IDs
	}
	return changed, nil
}

// instance is a registry row plus derived paths, as drivers see it.
type instance struct {
	registry.Service
	cfg  config
	root string
}

func (m *Manager) instance(row registry.Service) instance {
	in := instance{Service: row, root: m.Root}
	if row.Config == "" {
		return in
	}
	if err := json.Unmarshal([]byte(row.Config), &in.cfg); err != nil {
		// Unparseable config = no extras, but say so: its ports and
		// credentials silently fall back to defaults otherwise.
		m.Log.Warn("ignoring unparseable service config", "service", row.Name, "err", err)
	}
	return in
}

func (in instance) logFile() string { return filepath.Join(in.root, "logs", ProcName(in.Name)+".log") }
func (in instance) runDir() string  { return filepath.Join(in.root, "run", ProcName(in.Name)) }

// exe is a binary inside the instance's build: exe("bin", "mysqld").
func (in instance) exe(dir, name string) string {
	return filepath.Join(in.BinaryDir, dir, name+exeSuffix)
}

// valkeyServer finds valkey-server: the Linux tarball keeps it in bin/,
// the Windows zip at the top level.
func (in instance) valkeyServer() string {
	for _, dir := range []string{"bin", ""} {
		if p := in.exe(dir, "valkey-server"); fileExists(p) {
			return p
		}
	}
	return in.exe("bin", "valkey-server")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (m *Manager) init() {
	if m.phase == nil {
		m.phase = map[string]string{}
		m.pending = map[string]registry.Service{}
		m.lastErr = map[string]string{}
		m.opsCtx, m.stopOps = context.WithCancel(context.Background())
	}
}

// operation starts detached work (create, clone, start) that benchd's
// shutdown, the timeout, or cancelling task ends. task may be nil. done
// must be called when the work ends.
func (m *Manager) operation(timeout time.Duration, task *tasks.Task) (ctx context.Context, done func(), err error) {
	m.mu.Lock()
	m.init()
	if m.closed {
		m.mu.Unlock()
		return nil, nil, errors.New("benchd is shutting down")
	}
	m.ops.Add(1)
	base := m.opsCtx
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(base, timeout)
	stop := func() bool { return false }
	if task != nil {
		stop = context.AfterFunc(task.Context(), cancel)
	}
	return ctx, func() {
		stop()
		cancel()
		m.ops.Done()
	}, nil
}

// Close cancels in-flight creates, clones and starts and waits (bounded)
// for them to unwind, so no half-made instance outlives the daemon.
// Call it before stopping the supervisor.
func (m *Manager) Close() {
	m.mu.Lock()
	m.init()
	m.closed = true
	stop := m.stopOps
	m.mu.Unlock()
	stop()
	unwound := make(chan struct{})
	go func() {
		m.ops.Wait()
		close(unwound)
	}()
	select {
	case <-unwound:
	case <-time.After(closeGrace):
		m.Log.Warn("service operations still running at shutdown")
	}
}

// ---- queries ----------------------------------------------------------------

// Catalog lists the creatable service kinds with their channels on this
// platform. Kinds with no build for this OS/arch are left out.
func (m *Manager) Catalog() []api.ServiceType {
	out := []api.ServiceType{}
	for _, kind := range driverOrder {
		d := drivers[kind]
		var channels []string
		for _, b := range m.Bin.Channels(d.binary) {
			if !slices.Contains(channels, b.Channel) {
				channels = append(channels, b.Channel)
			}
		}
		if len(channels) == 0 {
			continue
		}
		out = append(out, api.ServiceType{Service: kind, Label: d.label, Channels: channels,
			DefaultPort: d.defaultPort, Singleton: d.singleton})
	}
	return out
}

// List reports every instance (including ones still being created).
func (m *Manager) List() ([]api.Service, error) {
	rows, err := m.Reg.Services()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.init()
	for _, p := range m.pending {
		rows = append(rows, p)
	}
	phase, lastErr := maps.Clone(m.phase), maps.Clone(m.lastErr)
	m.mu.Unlock()

	slices.SortFunc(rows, func(a, b registry.Service) int { return strings.Compare(a.Name, b.Name) })
	out := make([]api.Service, 0, len(rows)) // never null on the wire
	for _, r := range rows {
		out = append(out, m.toAPI(r, phase[r.Name], lastErr[r.Name]))
	}
	return out, nil
}

// Get reports one instance.
func (m *Manager) Get(name string) (api.Service, error) {
	list, err := m.List()
	if err != nil {
		return api.Service{}, err
	}
	for _, s := range list {
		if s.Name == name {
			return s, nil
		}
	}
	return api.Service{}, fmt.Errorf("%w: %q", registry.ErrServiceNotFound, name)
}

func (m *Manager) toAPI(r registry.Service, phase, lastErr string) api.Service {
	in := m.instance(r)
	s := api.Service{
		Name: r.Name, Service: r.Kind, Channel: r.Channel, Port: r.Port, Ports: in.cfg.Ports,
		Autostart: r.Autostart, DataDir: r.DataDir, BinDir: r.BinaryDir, LogFile: in.logFile(), Env: []string{},
	}
	if r.BinaryDir != "" {
		s.Version = filepath.Base(r.BinaryDir)
	}
	if in.cfg.UIPassword != "" {
		s.UIAuth = mailpitUser + ":" + in.cfg.UIPassword
	}
	if d, ok := drivers[r.Kind]; ok && r.Port != 0 {
		s.Env = d.env(in)
		s.URL = desktop.DatabaseURL(s)
		if argv := desktop.TerminalClient(s, runtime.GOOS); argv != nil && r.BinaryDir != "" {
			s.Client = desktop.Quote(argv)
		}
		if d.console != nil {
			s.ConsoleURL, s.ConsoleLogin = d.console(in)
		}
		if r.Port != d.defaultPort {
			// Derived, not stored: survives restarts and can't go stale.
			s.Notice = fmt.Sprintf("listens on port %d, not the default %d: use this port in your .env", r.Port, d.defaultPort)
		}
	}
	s.State = string(supervisor.StateStopped)
	if st, ok := m.Sup.Status(ProcName(r.Name)); ok {
		s.State, s.PID = string(st.State), st.PID
		if st.State == supervisor.StateFailed || st.State == supervisor.StateBackoff {
			s.Error = st.Error
		}
	}
	if phase != "" {
		s.State = phase
	}
	if lastErr != "" {
		s.Error = lastErr
	}
	return s
}

// publish pushes the current list to OnChange. pubMu spans the read and
// the send, so concurrent publishes can't deliver an older list last.
func (m *Manager) publish() {
	if m.OnChange == nil {
		return
	}
	m.pubMu.Lock()
	defer m.pubMu.Unlock()
	list, err := m.List()
	if err != nil {
		m.Log.Warn("listing services for event", "err", err)
		return
	}
	m.OnChange(list)
}

// Notify is wired to supervisor state changes of instance processes.
func (m *Manager) Notify() { m.publish() }

// ---- phase bookkeeping -------------------------------------------------------

// begin marks names as busy with phase; it fails if any already is.
func (m *Manager) begin(phase string, names ...string) error {
	m.mu.Lock()
	m.init()
	for _, n := range names {
		if p, busy := m.phase[n]; busy {
			m.mu.Unlock()
			return fmt.Errorf("%w: %s is %s", ErrBusy, n, p)
		}
	}
	for _, n := range names {
		m.phase[n] = phase
	}
	m.mu.Unlock()
	m.publish()
	return nil
}

// checkIdle fails when name is mid-operation.
func (m *Manager) checkIdle(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	if p, busy := m.phase[name]; busy {
		return fmt.Errorf("%w: %s is %s", ErrBusy, name, p)
	}
	return nil
}

func (m *Manager) setPhase(name, phase string) {
	m.mu.Lock()
	m.phase[name] = phase
	m.mu.Unlock()
	m.publish()
}

func (m *Manager) end(names ...string) {
	m.mu.Lock()
	for _, n := range names {
		delete(m.phase, n)
		delete(m.pending, n)
	}
	m.mu.Unlock()
	m.publish()
}

func (m *Manager) setLastErr(name string, err error) {
	m.mu.Lock()
	m.init()
	if err == nil {
		delete(m.lastErr, name)
	} else {
		m.lastErr[name] = err.Error()
	}
	m.mu.Unlock()
}

// ---- create -------------------------------------------------------------------

// Create installs (if needed), initializes and starts a new instance. It
// runs as a task that can be cancelled, through the task only, until
// initialization begins.
func (m *Manager) Create(req api.CreateServiceRequest) (svc api.Service, err error) {
	d, ok := drivers[req.Service]
	if !ok {
		return api.Service{}, inputErrorf("unknown service %q (available: %s)", req.Service, strings.Join(driverOrder, ", "))
	}
	name := req.Name
	if name == "" {
		name = req.Service
	}
	name, err = normalizeName(name)
	if err != nil {
		return api.Service{}, err
	}
	channel, err := m.channel(d, req.Channel)
	if err != nil {
		return api.Service{}, err
	}
	if err := preflight(d); err != nil {
		return api.Service{}, err
	}

	row := registry.Service{Kind: req.Service, Channel: channel, Name: name, Autostart: req.Autostart,
		DataDir: filepath.Join(m.Root, "data", req.Service, name)}
	cfg := config{}
	if _, err := fillSecrets(req.Service, &cfg); err != nil {
		return api.Service{}, err
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return api.Service{}, err
	}
	row.Config = string(cfgJSON)
	if err := m.reserve(row, d.singleton); err != nil {
		return api.Service{}, err
	}
	created := false
	defer func() {
		if !created {
			m.end(name)
		}
	}()
	task := m.Tasks.Begin("service.create", fmt.Sprintf("Creating %s %s (%s)", d.label, channel, name), name, true)
	defer func() { err = task.End(err) }()
	ctx, done, err := m.operation(createTimeout, task)
	if err != nil {
		return api.Service{}, err
	}
	defer done()

	// A bad explicit port must fail before a minutes-long download.
	if req.Port != 0 {
		if err := m.checkExplicitPort(name, req.Port); err != nil {
			return api.Service{}, err
		}
	}
	if b, err := m.Bin.Resolve(d.binary, channel); err == nil {
		task.Watch(d.binary, b.Version)
	}
	task.SetPhase("downloading")
	b, err := m.Bin.Install(ctx, d.binary, channel)
	if err != nil {
		return api.Service{}, fmt.Errorf("installing %s %s: %w", d.label, channel, err)
	}
	row.BinaryDir = m.Bin.Dir(d.binary, b.Version)

	if err := m.allocate(&row, d, req.Port); err != nil {
		return api.Service{}, err
	}

	// From here on the instance gets a data dir and a registry row; an
	// interruption would leave a half-made instance behind.
	if err := task.Uncancellable(); err != nil {
		return api.Service{}, err
	}
	task.SetPhase("initializing")
	m.setPhase(name, "initializing")
	if err := m.initNew(ctx, d, m.instance(row)); err != nil {
		return api.Service{}, err
	}
	if err := m.Reg.InsertService(row); err != nil {
		return api.Service{}, err
	}
	created = true
	m.end(name)

	task.SetPhase("starting")
	if err := m.start(ctx, row); err != nil {
		return m.mustGet(name), fmt.Errorf("created %s but it failed to start: %w", name, err)
	}
	return m.mustGet(name), nil
}

// channel resolves "" to the newest catalog channel and validates the rest.
func (m *Manager) channel(d driver, want string) (string, error) {
	builds := m.Bin.Channels(d.binary)
	if len(builds) == 0 {
		return "", inputErrorf("%s is not available for %s/%s yet", d.label, runtime.GOOS, runtime.GOARCH)
	}
	if want == "" {
		return builds[len(builds)-1].Channel, nil
	}
	for _, b := range builds {
		if b.Channel == want || b.Version == want {
			return b.Channel, nil
		}
	}
	var have []string
	for _, b := range builds {
		if !slices.Contains(have, b.Channel) {
			have = append(have, b.Channel)
		}
	}
	return "", inputErrorf("no %s %s build (available: %s)", d.label, want, strings.Join(have, ", "))
}

// reserve claims a name for a create/clone: not in the registry, not
// already pending, and (for singletons) no other instance of the kind.
func (m *Manager) reserve(row registry.Service, singleton bool) error {
	rows, err := m.Reg.Services()
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.init()
	for _, p := range m.pending {
		rows = append(rows, p)
	}
	if p, busy := m.phase[row.Name]; busy {
		// e.g. a delete still removing this name's data
		m.mu.Unlock()
		return fmt.Errorf("%w: %s is %s", ErrBusy, row.Name, p)
	}
	for _, r := range rows {
		if r.Name == row.Name {
			m.mu.Unlock()
			return inputErrorf("a service instance named %q already exists", row.Name)
		}
		if singleton && r.Kind == row.Kind {
			m.mu.Unlock()
			return inputErrorf("only one %s instance is allowed (%q exists)", drivers[row.Kind].label, r.Name)
		}
	}
	m.pending[row.Name] = row
	m.phase[row.Name] = "installing"
	m.mu.Unlock()
	m.publish()
	return nil
}

func (m *Manager) updatePending(row registry.Service) {
	m.mu.Lock()
	if _, ok := m.pending[row.Name]; ok {
		m.pending[row.Name] = row
	}
	m.mu.Unlock()
}

// allocate assigns ports and records them on the pending row in one
// critical section, so two concurrent creates can't pick the same port.
func (m *Manager) allocate(row *registry.Service, d driver, requested int) error {
	m.allocMu.Lock()
	defer m.allocMu.Unlock()
	if err := m.assignPorts(row, d, requested); err != nil {
		return err
	}
	m.updatePending(*row)
	return nil
}

// checkExplicitPort validates a user-chosen port early (allocate checks
// it again when it actually assigns).
func (m *Manager) checkExplicitPort(name string, port int) error {
	taken, err := m.takenPorts(name)
	if err != nil {
		return err
	}
	_, err = allocatePort(port, true, taken, portFree)
	return err
}

// assignPorts picks the main port (requested or default, moved past busy
// ones) and every secondary port. Other config fields are kept.
func (m *Manager) assignPorts(row *registry.Service, d driver, requested int) error {
	taken, err := m.takenPorts(row.Name)
	if err != nil {
		return err
	}
	want, explicit := d.defaultPort, requested != 0
	if explicit {
		want = requested
	}
	port, err := allocatePort(want, explicit, taken, portFree)
	if err != nil {
		return err
	}
	taken[port] = true
	row.Port = port

	cfg := m.instance(*row).cfg
	cfg.Ports = nil
	for _, x := range d.extraPorts {
		p, err := allocatePort(x.defaultPort, false, taken, portFree)
		if err != nil {
			return err
		}
		taken[p] = true
		if cfg.Ports == nil {
			cfg.Ports = map[string]int{}
		}
		cfg.Ports[x.role] = p
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	row.Config = string(b)
	return nil
}

// takenPorts collects every port assigned to another instance.
func (m *Manager) takenPorts(except string) (map[int]bool, error) {
	// Pending first, registry second: a create that commits in between
	// (insert, then leave pending) is then seen in at least one of them.
	m.mu.Lock()
	m.init()
	var rows []registry.Service
	for _, p := range m.pending {
		rows = append(rows, p)
	}
	m.mu.Unlock()
	registered, err := m.Reg.Services()
	if err != nil {
		return nil, err
	}
	rows = append(rows, registered...)
	taken := map[int]bool{}
	for _, r := range rows {
		if r.Name == except {
			continue
		}
		taken[r.Port] = true
		for _, p := range m.instance(r).cfg.Ports {
			taken[p] = true
		}
	}
	return taken, nil
}

// initNew prepares the data dir of an instance being created. Kept data
// (a marked dir of the same kind and channel, left by a kept-data
// delete) is reused. Leftovers of a crashed create are wiped. Anything
// else in the way is refused: Bench never deletes data it didn't make.
func (m *Manager) initNew(ctx context.Context, d driver, in instance) error {
	kind, channel, err := readMarker(in.DataDir)
	switch {
	case err == nil && kind == in.Kind && (channel == "" || channel == in.Channel):
		return ensureDirs(in)
	case err == nil && kind == in.Kind:
		return inputErrorf("%s holds %s %s data from a deleted instance; create %s with version %s to reuse it, or delete that directory",
			in.DataDir, drivers[kind].label, channel, in.Name, channel)
	case err == nil:
		return inputErrorf("%s holds %s data; pick another name or delete that directory", in.DataDir, kind)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("reading %s marker: %w", in.DataDir, err)
	}
	if err := m.safeDataDir(in.DataDir); err != nil {
		return err
	}
	sentinel := in.DataDir + creatingSuffix
	if !dirEmpty(in.DataDir) && !fileExists(sentinel) {
		return inputErrorf("%s already exists and Bench didn't create it; move it away or pick another name", in.DataDir)
	}
	if err := os.MkdirAll(filepath.Dir(in.DataDir), 0o755); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	if err := os.WriteFile(sentinel, nil, 0o644); err != nil {
		return fmt.Errorf("marking create in progress: %w", err)
	}
	if err := os.RemoveAll(in.DataDir); err != nil {
		return fmt.Errorf("clearing an interrupted create: %w", err)
	}
	if err := ensureDirs(in); err != nil {
		return err
	}
	if d.prepare != nil {
		if err := d.prepare(in); err != nil {
			return err
		}
	}
	if d.init != nil {
		// initdb and mysqld --initialize want to create the dir themselves
		// (initdb refuses a non-empty one; both set its permissions).
		if err := d.init(ctx, in); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(in.DataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	if err := writeMarker(in); err != nil {
		return err
	}
	return os.Remove(sentinel)
}

// checkInitialized is start's guard, and it never wipes: a registered
// instance's data dir always carries the marker. Without one, the data
// was restored or edited by hand, and only the user can say what it is.
// An empty or missing dir (the data was deleted) is initialized afresh.
func (m *Manager) checkInitialized(ctx context.Context, d driver, in instance) error {
	kind, _, err := readMarker(in.DataDir)
	switch {
	case err == nil && kind == in.Kind:
		return nil
	case err == nil:
		return fmt.Errorf("%s holds %s data, not %s; refusing to start %s", in.DataDir, kind, in.Kind, in.Name)
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("reading %s marker: %w", in.DataDir, err)
	case !dirEmpty(in.DataDir) && !fileExists(in.DataDir+creatingSuffix):
		// (With the sentinel present, this is an interrupted re-init of a
		// deleted dir: initNew below finishes it.)
		return fmt.Errorf("%s has data but no %s file, so Bench won't touch it; if it is %s's data, create that file containing %q and start again",
			in.DataDir, markerFile, in.Name, in.Kind+" "+in.Channel)
	}
	return m.initNew(ctx, d, in)
}

func readMarker(dir string) (kind, channel string, err error) {
	b, err := os.ReadFile(filepath.Join(dir, markerFile))
	if err != nil {
		return "", "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", "", fmt.Errorf("empty %s", markerFile)
	}
	if len(fields) > 1 {
		channel = fields[1]
	}
	return fields[0], channel, nil
}

func writeMarker(in instance) error {
	if err := os.WriteFile(filepath.Join(in.DataDir, markerFile), []byte(in.Kind+" "+in.Channel+"\n"), 0o644); err != nil {
		return fmt.Errorf("marking data dir initialized: %w", err)
	}
	return nil
}

// ensureDirs creates the instance's scratch and log dirs (every start:
// a clone has no run dir yet, and mysqld needs it for its socket).
func ensureDirs(in instance) error {
	for _, dir := range []string{in.runDir(), filepath.Join(in.root, "logs")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return nil
}

// dirEmpty reports whether dir is missing or has no entries.
func dirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}

// randomKey returns a hex secret for service credentials.
func randomKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating a key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// normalizeName validates an instance name: one DNS label (the same rule
// as site names), short enough for socket paths.
func normalizeName(name string) (string, error) {
	n, err := sites.NormalizeName(name)
	if err != nil {
		return "", InputError{msg: err.Error()}
	}
	if len(n) > maxNameLen {
		return "", inputErrorf("instance name %q is too long (max %d characters)", n, maxNameLen)
	}
	return n, nil
}

// safeDataDir refuses to delete anything outside <root>/data: a corrupt
// registry row must never turn into RemoveAll on a user directory.
func (m *Manager) safeDataDir(dir string) error {
	base := filepath.Clean(filepath.Join(m.Root, "data")) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(dir)+string(os.PathSeparator), base) || filepath.Clean(dir)+string(os.PathSeparator) == base {
		return fmt.Errorf("refusing to manage data dir %s outside %s", dir, base)
	}
	return nil
}

func (m *Manager) mustGet(name string) api.Service {
	s, err := m.Get(name)
	if err != nil {
		return api.Service{Name: name, Error: err.Error()}
	}
	return s
}

// ---- start / stop ---------------------------------------------------------------

// Start starts an instance (idempotent while running). The instance is
// held in the "starting" phase so a concurrent stop/delete/clone waits
// its turn instead of racing the spawn. The work is detached from ctx.
func (m *Manager) Start(ctx context.Context, name string) (api.Service, error) {
	if err := m.begin("starting", name); err != nil {
		return api.Service{}, err
	}
	err := m.startHeld(name)
	m.end(name)
	if errors.Is(err, registry.ErrServiceNotFound) {
		return api.Service{}, err
	}
	return m.mustGet(name), err
}

// startHeld starts name while the caller holds its phase; it re-reads the
// row so it acts on what's registered now.
func (m *Manager) startHeld(name string) error {
	row, err := m.Reg.Service(name)
	if err != nil {
		return err
	}
	ctx, done, err := m.operation(startTimeout, nil)
	if err != nil {
		return err
	}
	defer done()
	return m.start(ctx, row)
}

// start brings one instance up: reinstall a missing build, reinitialize a
// missing dataset, check the port, then hand the process to the supervisor.
func (m *Manager) start(ctx context.Context, row registry.Service) (err error) {
	defer func() {
		m.setLastErr(row.Name, err)
		m.publish()
	}()
	d, ok := drivers[row.Kind]
	if !ok {
		return fmt.Errorf("instance %s has unknown service kind %q", row.Name, row.Kind)
	}
	if err := preflight(d); err != nil {
		return err
	}
	if !fileExists(row.BinaryDir) {
		b, err := m.Bin.Install(ctx, d.binary, row.Channel)
		if err != nil {
			return fmt.Errorf("reinstalling %s %s: %w", d.label, row.Channel, err)
		}
		row.BinaryDir = m.Bin.Dir(d.binary, b.Version)
		if err := m.Reg.SetServiceBinaryDir(row.Name, row.BinaryDir); err != nil {
			return err
		}
	}
	in := m.instance(row)
	if changed, err := fillSecrets(row.Kind, &in.cfg); err != nil {
		return err
	} else if changed {
		b, err := json.Marshal(in.cfg)
		if err != nil {
			return err
		}
		if err := m.Reg.SetServiceConfig(row.Name, string(b)); err != nil {
			return err
		}
		in.Config = string(b)
	}
	if err := ensureDirs(in); err != nil {
		return err
	}
	if d.prepare != nil {
		if err := d.prepare(in); err != nil {
			return err
		}
	}
	if err := m.checkInitialized(ctx, d, in); err != nil {
		return err
	}
	if st, ok := m.Sup.Status(ProcName(row.Name)); !ok || st.State == supervisor.StateStopped || st.State == supervisor.StateFailed {
		if !portFree(row.Port) {
			return fmt.Errorf("port %d is in use by another process; stop it or recreate %s on another port", row.Port, row.Name)
		}
	}
	spec := d.spec(in)
	spec.Name = ProcName(row.Name)
	spec.LogFile = in.logFile()
	spec.StopTimeout = d.stopTimeout
	logStart := fileSize(spec.LogFile) // hints only look at this attempt's output
	if err := m.Sup.Start(ctx, spec); err != nil {
		return fmt.Errorf("%w%s", err, hint(err.Error()+logSince(spec.LogFile, logStart)))
	}
	// Start "succeeds" at once for a process that became ready once and
	// now crash-loops; that's a failure to report, not a start.
	if st, ok := m.Sup.Status(spec.Name); ok && st.State == supervisor.StateBackoff {
		return fmt.Errorf("%s keeps exiting (%s); see %s%s", row.Name, st.Error, spec.LogFile, hint(logSince(spec.LogFile, logStart)))
	}
	if d.afterStart != nil {
		if err := d.afterStart(ctx, in); err != nil {
			return fmt.Errorf("%s started, but %w", row.Name, err)
		}
	}
	return nil
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// logSince returns what was appended to a log after offset (its last 4 KB).
func logSince(path string, offset int64) string {
	b, err := os.ReadFile(path)
	if err != nil || int64(len(b)) < offset {
		return ""
	}
	b = b[offset:]
	if len(b) > 4096 {
		b = b[len(b)-4096:]
	}
	return string(b)
}

// Stop stops an instance (a no-op when not running).
func (m *Manager) Stop(name string) (api.Service, error) {
	if err := m.begin("stopping", name); err != nil {
		return api.Service{}, err
	}
	if _, err := m.Reg.Service(name); err != nil {
		m.end(name)
		return api.Service{}, err
	}
	m.Sup.Stop(ProcName(name))
	m.setLastErr(name, nil)
	m.end(name)
	return m.mustGet(name), nil
}

// SetAutostart toggles starting the instance when benchd boots.
func (m *Manager) SetAutostart(name string, enabled bool) (api.Service, error) {
	if err := m.checkIdle(name); err != nil {
		return api.Service{}, err
	}
	if err := m.Reg.SetServiceAutostart(name, enabled); err != nil {
		return api.Service{}, err
	}
	m.publish()
	return m.mustGet(name), nil
}

// StartAutostart starts every autostart instance (benchd boot). Failures
// are recorded per instance and logged; one broken instance doesn't stop
// the rest.
func (m *Manager) StartAutostart() {
	rows, err := m.Reg.Services()
	if err != nil {
		m.Log.Error("listing services for autostart", "err", err)
		return
	}
	for _, r := range rows {
		if !r.Autostart {
			continue
		}
		if err := m.begin("starting", r.Name); err != nil {
			continue // already being worked on by an API call
		}
		row, err := m.Reg.Service(r.Name)
		if err == nil && row.Autostart {
			err = m.startHeld(r.Name)
		}
		m.end(r.Name)
		if err != nil && !errors.Is(err, registry.ErrServiceNotFound) {
			m.Log.Error("autostart failed", "service", r.Name, "err", err)
		}
	}
}

// ---- delete / clone ---------------------------------------------------------------

// Delete stops and removes an instance, and its data unless keepData.
func (m *Manager) Delete(name string, keepData bool) error {
	if err := m.begin("deleting", name); err != nil {
		return err
	}
	defer m.end(name)
	row, err := m.Reg.Service(name)
	if err != nil {
		return err
	}
	m.Sup.Stop(ProcName(name))
	if err := m.Reg.DeleteService(name); err != nil {
		return err
	}
	m.setLastErr(name, nil)
	if keepData {
		return nil
	}
	if err := m.safeDataDir(row.DataDir); err != nil {
		return err
	}
	if err := os.RemoveAll(row.DataDir); err != nil {
		return fmt.Errorf("removing data of %s: %w", name, err)
	}
	_ = os.RemoveAll(m.instance(row).runDir()) // scratch dir; nothing to lose
	_ = os.Remove(m.instance(row).logFile())   // the instance is gone; so is its log
	return nil
}

// Clone copies an instance with its data into a new instance. A running source is stopped for a consistent copy and
// started again afterwards; it stays in the "cloning" phase until then.
func (m *Manager) Clone(srcName, newName string) (svc api.Service, err error) {
	src, err := m.Reg.Service(srcName)
	if err != nil {
		return api.Service{}, err
	}
	d := drivers[src.Kind]
	if d.singleton {
		return api.Service{}, inputErrorf("%s can't be cloned (only one instance is allowed)", d.label)
	}
	name, err := normalizeName(newName)
	if err != nil {
		return api.Service{}, err
	}
	row := registry.Service{Kind: src.Kind, Channel: src.Channel, Name: name, BinaryDir: src.BinaryDir,
		Config: src.Config, DataDir: filepath.Join(m.Root, "data", src.Kind, name)}
	if fileExists(row.DataDir) {
		// Likely data kept by an earlier delete: never overwrite it.
		return api.Service{}, inputErrorf("%s already exists (data kept from a deleted instance?); pick another name or delete that directory", row.DataDir)
	}
	if err := m.reserve(row, false); err != nil {
		return api.Service{}, err
	}
	created := false
	defer func() {
		if !created {
			m.end(name)
		}
	}()
	if err := m.begin("cloning", srcName); err != nil {
		return api.Service{}, err
	}
	srcHeld := true
	releaseSrc := func() {
		if srcHeld {
			srcHeld = false
			m.end(srcName)
		}
	}
	defer releaseSrc()
	m.setPhase(name, "cloning")
	// Tracked, never cancellable: the copy can't be interrupted and the
	// source must be restarted whatever happens.
	task := m.Tasks.Begin("service.clone", fmt.Sprintf("Cloning %s to %s", srcName, name), name, false)
	defer func() { err = task.End(err) }()
	task.SetPhase("copying")
	// Not tied to the request: a dropped client must not leave the source stopped.
	ctx, done, err := m.operation(createTimeout, nil)
	if err != nil {
		return api.Service{}, err
	}
	defer done()

	// Anything the supervisor still owns (running, starting, or waiting to
	// restart after a crash) must stop, or it could write mid-copy.
	st, known := m.Sup.Status(ProcName(srcName))
	wasRunning := known && st.State != supervisor.StateStopped && st.State != supervisor.StateFailed
	if wasRunning {
		m.Sup.Stop(ProcName(srcName))
	}
	copyErr := m.copyData(d, src.DataDir, row.DataDir)
	var restartErr error
	if wasRunning {
		// Restart the source whatever happened to the copy.
		if err := m.start(ctx, src); err != nil {
			restartErr = fmt.Errorf("restarting %s after the copy failed: %w", srcName, err)
			if copyErr == nil {
				restartErr = fmt.Errorf("the copy succeeded, but restarting %s afterwards failed: %w", srcName, err)
			}
		}
	}
	releaseSrc()
	if copyErr != nil {
		return api.Service{}, errors.Join(copyErr, restartErr)
	}

	if err := m.allocate(&row, d, 0); err != nil {
		_ = os.RemoveAll(row.DataDir) // the copy just made it
		return api.Service{}, errors.Join(err, restartErr)
	}
	if err := m.Reg.InsertService(row); err != nil {
		_ = os.RemoveAll(row.DataDir)
		return api.Service{}, errors.Join(err, restartErr)
	}
	created = true
	m.end(name)
	var startErr error
	if err := m.start(ctx, row); err != nil {
		startErr = fmt.Errorf("cloned %s but it failed to start: %w", name, err)
	}
	return m.mustGet(name), errors.Join(startErr, restartErr)
}

// copyData duplicates a data dir into <to>.partial, lets the driver scrub
// identity files (server UUIDs, pid files), and only then renames it into
// place: an interrupted clone never leaves a marked, half-copied dataset.
func (m *Manager) copyData(d driver, from, to string) error {
	if err := m.safeDataDir(to); err != nil {
		return err
	}
	partial := to + ".partial"
	if err := os.RemoveAll(partial); err != nil { // an interrupted clone's leftovers
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	err := func() error {
		if err := os.CopyFS(partial, os.DirFS(from)); err != nil {
			return fmt.Errorf("copying data: %w", err)
		}
		if err := os.Chmod(partial, 0o700); err != nil {
			return err
		}
		if d.afterClone != nil {
			if err := d.afterClone(partial); err != nil {
				return fmt.Errorf("preparing cloned data: %w", err)
			}
		}
		return os.Rename(partial, to)
	}()
	if err != nil {
		_ = os.RemoveAll(partial)
	}
	return err
}
