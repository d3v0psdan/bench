// Package supervisor runs benchd's long-lived children (Caddy, PHP pools,
// services): spawn, readiness gate, crash restart with capped exponential
// backoff, and stdout/stderr capture to a log file. Everything benchd
// spawns long-lived goes through here, never a bare exec.Command.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// State is a supervised process's lifecycle phase.
type State string

const (
	StateStarting State = "starting" // spawned, waiting for readiness
	StateRunning  State = "running"
	StateBackoff  State = "backoff" // exited, waiting to restart
	StateStopped  State = "stopped" // stopped on request
	StateFailed   State = "failed"  // never became ready; not retrying
)

// minSignalGrace is the least time a process gets between the OS stop
// signal and a hard kill, even when a clean Shutdown used up the grace.
const minSignalGrace = time.Second

// ErrStopped reports that a process was stopped before it became ready.
var ErrStopped = errors.New("process stopped before becoming ready")

// Spec describes a child process to supervise. Name must be unique per
// Supervisor.
type Spec struct {
	Name    string
	Command string // absolute path to the binary
	Args    []string
	Env     []string // appended to the daemon's environment (overrides on conflict)
	Dir     string   // working directory ("" = inherit)
	LogFile string   // stdout+stderr appended here ("" = discard)
	// Output, for Exec only, also receives stdout+stderr as they are
	// written, so a long tool (composer, npm) can be followed live.
	Output io.Writer
	// Health reports whether the process is ready to serve (e.g. a TCP
	// dial). nil means ready as soon as it has spawned.
	Health func(ctx context.Context) error
	// Shutdown, if set, asks the process to exit cleanly (mysqladmin
	// shutdown, pg_ctl stop) before any OS-level stop. Windows has no
	// SIGTERM, so without this a database would crash-recover on every
	// stop. The process gets StopTimeout to exit after Shutdown returns.
	Shutdown func(ctx context.Context) error
	// StopTimeout overrides the supervisor's grace period (0 = default).
	StopTimeout time.Duration
}

// Status is a point-in-time snapshot of a supervised process.
type Status struct {
	Name     string `json:"name"`
	State    State  `json:"state"`
	PID      int    `json:"pid,omitempty"`
	Restarts int    `json:"restarts"`
	Error    string `json:"error,omitempty"`
}

// Supervisor owns a set of supervised processes. Create with New.
type Supervisor struct {
	// Tuning knobs, set before the first Start (tests shrink them).
	ReadyTimeout   time.Duration // per-attempt readiness deadline
	HealthInterval time.Duration // readiness poll interval
	BackoffMin     time.Duration
	BackoffMax     time.Duration
	BackoffReset   time.Duration // uptime after which backoff resets
	StopTimeout    time.Duration // graceful-stop grace before hard kill

	// OnEvent, if set, is called on every state change (feeds the WS
	// event stream). Called from monitor goroutines; must not block.
	OnEvent func(Status)

	log    *slog.Logger
	mu     sync.Mutex
	procs  map[string]*proc
	closed bool // StopAll ran; no new children may spawn
}

// New returns a Supervisor with production defaults.
func New(log *slog.Logger) *Supervisor {
	return &Supervisor{
		ReadyTimeout:   15 * time.Second,
		HealthInterval: 250 * time.Millisecond,
		BackoffMin:     500 * time.Millisecond,
		BackoffMax:     30 * time.Second,
		BackoffReset:   30 * time.Second,
		StopTimeout:    5 * time.Second,
		log:            log,
		procs:          map[string]*proc{},
	}
}

type proc struct {
	spec   Spec
	cancel context.CancelFunc
	done   chan struct{} // closed when the monitor goroutine exits

	readyOnce sync.Once
	ready     chan struct{} // closed at first readiness (or terminal failure)
	readyErr  error         // set before ready closes

	mu       sync.Mutex
	state    State
	pid      int
	restarts int
	lastErr  error
}

func (p *proc) signalReady(err error) {
	p.readyOnce.Do(func() {
		p.readyErr = err
		close(p.ready)
	})
}

func (p *proc) setPID(pid int) {
	p.mu.Lock()
	p.pid = pid
	p.mu.Unlock()
}

func (p *proc) status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{Name: p.spec.Name, State: p.state, Restarts: p.restarts}
	if p.state == StateStarting || p.state == StateRunning {
		st.PID = p.pid
	}
	if p.lastErr != nil {
		st.Error = p.lastErr.Error()
	}
	return st
}

// setState records a transition and fires OnEvent. backoff/failed retain
// their cause; entering running or stopped clears it: a deliberately
// stopped process must not keep reporting its old crash error.
func (s *Supervisor) setState(p *proc, state State, err error) {
	p.mu.Lock()
	p.state = state
	if err != nil || state == StateRunning || state == StateStopped {
		p.lastErr = err
	}
	p.mu.Unlock()
	if s.OnEvent != nil {
		s.OnEvent(p.status())
	}
}

// Start launches (or joins) the named process and blocks until it is ready,
// the first start fails, or ctx is done. Idempotent: starting an already
// running process returns nil once it is ready. A process whose first
// attempt never becomes ready is failed terminally; restart-with-backoff
// only applies after a successful first readiness (a bad binary or config
// should surface as an error, not a silent crash loop).
func (s *Supervisor) Start(ctx context.Context, spec Spec) error {
	if spec.Name == "" || spec.Command == "" {
		return errors.New("supervisor spec needs Name and Command")
	}
	s.mu.Lock()
	if s.closed {
		// A Start racing StopAll (e.g. async site restore during daemon
		// shutdown) must not spawn a child that outlives benchd.
		s.mu.Unlock()
		return errors.New("supervisor is shut down")
	}
	if p, ok := s.procs[spec.Name]; ok {
		st := p.status().State
		if st != StateStopped && st != StateFailed {
			s.mu.Unlock()
			select {
			case <-p.ready:
				return p.readyErr
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	pctx, cancel := context.WithCancel(context.Background())
	p := &proc{
		spec:   spec,
		cancel: cancel,
		done:   make(chan struct{}),
		ready:  make(chan struct{}),
		state:  StateStarting,
	}
	s.procs[spec.Name] = p
	s.mu.Unlock()

	go s.monitor(pctx, p)

	select {
	case <-p.ready:
		return p.readyErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop terminates the named process and waits for its monitor to exit.
// Unknown names are a no-op.
func (s *Supervisor) Stop(name string) {
	s.mu.Lock()
	p := s.procs[name]
	s.mu.Unlock()
	if p == nil {
		return
	}
	p.cancel()
	<-p.done
}

// StopAll terminates every process, waits for all monitors to exit, and
// closes the Supervisor to further Starts.
func (s *Supervisor) StopAll() {
	s.mu.Lock()
	s.closed = true
	procs := make([]*proc, 0, len(s.procs))
	for _, p := range s.procs {
		procs = append(procs, p)
	}
	s.mu.Unlock()
	for _, p := range procs {
		p.cancel()
	}
	for _, p := range procs {
		<-p.done
	}
}

// Status returns the named process's snapshot.
func (s *Supervisor) Status(name string) (Status, bool) {
	s.mu.Lock()
	p := s.procs[name]
	s.mu.Unlock()
	if p == nil {
		return Status{}, false
	}
	return p.status(), true
}

// Statuses returns all known processes, sorted by name.
func (s *Supervisor) Statuses() []Status {
	s.mu.Lock()
	sts := make([]Status, 0, len(s.procs))
	for _, p := range s.procs {
		sts = append(sts, p.status())
	}
	s.mu.Unlock()
	slices.SortFunc(sts, func(a, b Status) int { return strings.Compare(a.Name, b.Name) })
	return sts
}

func (s *Supervisor) monitor(ctx context.Context, p *proc) {
	defer close(p.done)
	backoff := s.BackoffMin
	for first := true; ; first = false {
		began := time.Now()
		ready, err := s.runOnce(ctx, p)
		if ctx.Err() != nil {
			s.setState(p, StateStopped, nil)
			p.signalReady(fmt.Errorf("%s: %w", p.spec.Name, ErrStopped))
			s.log.Info("supervised process stopped", "name", p.spec.Name)
			return
		}
		if first && !ready {
			s.setState(p, StateFailed, err)
			p.signalReady(err)
			s.log.Error("supervised process failed to start", "name", p.spec.Name, "err", err)
			return
		}
		if time.Since(began) >= s.BackoffReset {
			backoff = s.BackoffMin
		}
		p.mu.Lock()
		p.restarts++
		p.mu.Unlock()
		s.setState(p, StateBackoff, err)
		s.log.Warn("supervised process exited, restarting",
			"name", p.spec.Name, "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			s.setState(p, StateStopped, nil)
			p.signalReady(fmt.Errorf("%s: %w", p.spec.Name, ErrStopped))
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, s.BackoffMax)
	}
}

// runOnce spawns the child once and blocks until it exits or ctx is done.
// ready reports whether this attempt passed the readiness gate.
func (s *Supervisor) runOnce(ctx context.Context, p *proc) (ready bool, err error) {
	cmd := exec.Command(p.spec.Command, p.spec.Args...)
	cmd.Dir = p.spec.Dir
	if len(p.spec.Env) > 0 {
		cmd.Env = append(os.Environ(), p.spec.Env...)
	}
	cmd.SysProcAttr = sysProcAttr()

	if p.spec.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(p.spec.LogFile), 0o755); err != nil {
			return false, fmt.Errorf("creating log directory: %w", err)
		}
		f, err := os.OpenFile(p.spec.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return false, fmt.Errorf("opening log file: %w", err)
		}
		defer f.Close()
		cmd.Stdout = f
		cmd.Stderr = f
	}

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("starting %s: %w", p.spec.Command, err)
	}
	p.setPID(cmd.Process.Pid)
	s.setState(p, StateStarting, nil)
	s.log.Info("supervised process started", "name", p.spec.Name, "pid", cmd.Process.Pid)

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	// stop terminates the child and drains its exit status: the spec's
	// clean Shutdown first (if any), then the OS signal, then a hard kill.
	// All of it fits one grace period, so a stop never takes longer than
	// the spec promises (`bench stop` budgets for exactly that).
	stop := func() {
		grace := s.StopTimeout
		if p.spec.StopTimeout > 0 {
			grace = p.spec.StopTimeout
		}
		deadline := time.Now().Add(grace)
		if p.spec.Shutdown != nil {
			sctx, cancel := context.WithDeadline(context.Background(), deadline)
			err := p.spec.Shutdown(sctx)
			cancel()
			if err == nil {
				select {
				case <-exited:
					return
				case <-time.After(time.Until(deadline)):
					err = fmt.Errorf("still running %s after clean shutdown", grace)
				}
			}
			s.log.Warn("clean shutdown failed, stopping forcefully", "name", p.spec.Name, "err", err)
		}
		signalStop(cmd.Process)
		select {
		case <-exited:
		case <-time.After(max(time.Until(deadline), minSignalGrace)):
			killHard(cmd.Process)
			<-exited
		}
	}

	if p.spec.Health != nil {
		deadline := time.NewTimer(s.ReadyTimeout)
		defer deadline.Stop()
		tick := time.NewTicker(s.HealthInterval)
		defer tick.Stop()
		var lastHealthErr error // the deadline error must carry the real cause
	probe:
		for {
			select {
			case <-ctx.Done():
				stop()
				return false, ctx.Err()
			case werr := <-exited:
				return false, fmt.Errorf("exited during startup: %w", exitReason(werr))
			case <-deadline.C:
				stop()
				if lastHealthErr != nil {
					return false, fmt.Errorf("not healthy after %s: %w", s.ReadyTimeout, lastHealthErr)
				}
				return false, fmt.Errorf("not healthy after %s", s.ReadyTimeout)
			case <-tick.C:
				hctx, cancel := context.WithTimeout(ctx, s.HealthInterval)
				herr := p.spec.Health(hctx)
				cancel()
				if herr == nil {
					break probe
				}
				lastHealthErr = herr
			}
		}
	}

	s.setState(p, StateRunning, nil)
	p.signalReady(nil)

	// Shortcut: restart triggers on process exit only; add periodic
	// health-probe restarts if a wedged-but-alive child shows up in practice.
	select {
	case <-ctx.Done():
		stop()
		return true, nil
	case werr := <-exited:
		return true, fmt.Errorf("process exited: %w", exitReason(werr))
	}
}

// exitReason normalizes cmd.Wait's result: exit status 0 is still an
// unexpected exit for a long-lived child.
func exitReason(werr error) error {
	if werr == nil {
		return errors.New("status 0")
	}
	return werr
}
