// Package tasks tracks benchd's long-running operations (binary downloads,
// service creates and clones, new-app creation, the HTTPS/DNS setup) so the
// GUI and CLI can watch their progress and cancel the ones that are safe to
// stop.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
)

var (
	// ErrNotFound: no running or recent task has that id.
	ErrNotFound = errors.New("no such task")
	// ErrNotCancellable: the task finished, or is in a step that must not
	// be interrupted (initializing a data dir, copying data).
	ErrNotCancellable = errors.New("task can't be cancelled now")
	// ErrCancelled is what a cancelled operation returns to its caller.
	ErrCancelled = errors.New("cancelled")
)

// Leftover is a failure that left something behind. End keeps What on a
// cancelled task too, so a cancel still says what is left (ui-standard F5).
type Leftover struct {
	Err  error
	What string
}

func (l Leftover) Error() string { return fmt.Sprintf("%v (%s)", l.Err, l.What) }
func (l Leftover) Unwrap() error { return l.Err }

// keepFinished bounds the history the Activity page shows.
const keepFinished = 20

// Registry holds running tasks and the most recent finished ones. The zero
// value is ready to use; a nil *Registry is valid and tracks nothing, so
// packages can take one optionally.
type Registry struct {
	// OnChange, if set, receives the full list after every change (feeds
	// the "tasks" WS event). Called with the registry locked, so updates
	// arrive in order; it must not block or call back into the registry.
	OnChange func([]api.Task)

	mu    sync.Mutex
	seq   int
	tasks []*entry // oldest first
}

type entry struct {
	task      api.Task
	cancel    context.CancelFunc
	binary    string // "name/version" whose download progress this task shows
	requested bool   // a cancel was requested
}

// Task is the owner's handle on one registered task.
type Task struct {
	r   *Registry
	e   *entry
	ctx context.Context
}

// Begin registers a running task. Its Context ends when the task is
// cancelled; the owner must call End exactly once.
func (r *Registry) Begin(kind, title, target string, cancellable bool) *Task {
	if r == nil {
		return &Task{ctx: context.Background()}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e := &entry{
		task: api.Task{
			ID: fmt.Sprintf("t%d", r.seq), Kind: kind, Title: title, Target: target,
			State: api.TaskRunning, Cancellable: cancellable, StartedAt: time.Now(),
		},
		cancel: cancel,
	}
	r.tasks = append(r.tasks, e)
	r.publish()
	return &Task{r: r, e: e, ctx: ctx}
}

// List returns running tasks and recent finished ones, newest first.
func (r *Registry) List() []api.Task {
	if r == nil {
		return []api.Task{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshot()
}

// Cancel asks a running, cancellable task to stop. The task stays
// "running" until its owner unwinds and calls End.
func (r *Registry) Cancel(id string) error {
	if r == nil {
		return ErrNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.tasks, func(e *entry) bool { return e.task.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	e := r.tasks[i]
	if e.task.State != api.TaskRunning || !e.task.Cancellable {
		return ErrNotCancellable
	}
	e.requested = true
	e.task.Cancellable = false
	e.task.Phase = "cancelling"
	e.cancel()
	r.publish()
	return nil
}

// Progress attaches a download update to every running task that watches
// that build. Wire it to binman.Manager.OnProgress.
func (r *Registry) Progress(p binman.Progress) {
	if r == nil {
		return
	}
	key := p.Name + "/" + p.Version
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, e := range r.tasks {
		if e.task.State == api.TaskRunning && e.binary == key {
			e.task.Download = &p
			changed = true
		}
	}
	if changed {
		r.publish()
	}
}

// Context ends when the task is cancelled (never, for a nil registry).
func (t *Task) Context() context.Context { return t.ctx }

// SetPhase names the current step ("downloading", "initializing").
func (t *Task) SetPhase(phase string) {
	t.update(func(e *entry) { e.task.Phase = phase })
}

// SetLogFile points clients at the file the task writes its output to.
func (t *Task) SetLogFile(path string) {
	t.update(func(e *entry) { e.task.LogFile = path })
}

// Snapshot is the task as clients see it right now (a nil registry's task
// is empty).
func (t *Task) Snapshot() api.Task {
	if t.r == nil {
		return api.Task{}
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	return t.e.task
}

// Watch shows download progress for the build name/version on this task.
func (t *Task) Watch(name, version string) {
	t.update(func(e *entry) { e.binary = name + "/" + version })
}

// Uncancellable marks the start of a step that must not be interrupted.
// It returns ErrCancelled if a cancel already landed, so the caller can
// stop before touching disk.
func (t *Task) Uncancellable() error {
	if t.r == nil {
		return nil
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	if t.e.requested {
		return ErrCancelled
	}
	if t.e.task.Cancellable {
		t.e.task.Cancellable = false
		t.r.publish()
	}
	return nil
}

// End records the outcome and returns the error the caller should report:
// ErrCancelled when the task was cancelled, err otherwise.
func (t *Task) End(err error) error {
	if t.r == nil {
		return err
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	e := t.e
	if e.task.State != api.TaskRunning {
		return err // End already called
	}
	now := time.Now()
	e.task.EndedAt = &now
	e.task.Cancellable = false
	e.task.Phase = ""
	switch {
	case e.requested && err != nil:
		e.task.State = api.TaskCancelled
		if l := (Leftover{}); errors.As(err, &l) {
			e.task.Error = l.What
		}
		err = ErrCancelled
	case err != nil:
		e.task.State = api.TaskFailed
		e.task.Error = err.Error()
	default:
		e.task.State = api.TaskDone
	}
	e.cancel()
	t.r.trim()
	t.r.publish()
	return err
}

func (t *Task) update(fn func(*entry)) {
	if t.r == nil {
		return
	}
	t.r.mu.Lock()
	defer t.r.mu.Unlock()
	fn(t.e)
	t.r.publish()
}

// trim drops the oldest finished tasks beyond keepFinished. Locked.
func (r *Registry) trim() {
	finished := 0
	for _, e := range r.tasks {
		if e.task.State != api.TaskRunning {
			finished++
		}
	}
	r.tasks = slices.DeleteFunc(r.tasks, func(e *entry) bool {
		if finished > keepFinished && e.task.State != api.TaskRunning {
			finished--
			return true
		}
		return false
	})
}

// snapshot copies the list, newest first. Locked.
func (r *Registry) snapshot() []api.Task {
	out := make([]api.Task, 0, len(r.tasks))
	for i := len(r.tasks) - 1; i >= 0; i-- {
		t := r.tasks[i].task
		if t.Download != nil {
			d := *t.Download
			t.Download = &d
		}
		out = append(out, t)
	}
	return out
}

// publish sends the list to OnChange. Locked.
func (r *Registry) publish() {
	if r.OnChange != nil {
		r.OnChange(r.snapshot())
	}
}
