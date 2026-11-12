package main

import (
	"sync"
	"time"
)

// lifeWork runs background work for the daemon's lifetime and lets
// shutdown wait for it, so what that work uses (the registry, the
// supervisor) isn't closed under it.
type lifeWork struct {
	grace time.Duration // how long Stop waits; 0 = stopGrace (tests shrink it)

	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup
	idle     chan struct{} // closed once running work has ended; made by the first Stop
	deadline time.Time     // when Stop gives up; set by the first Stop
}

// stopGrace bounds how long shutdown waits for background work. It covers
// a cancelled tool's exit (supervisor.Exec waits up to 5s for its output)
// and keeps `bench stop` inside its budget.
const stopGrace = 10 * time.Second

// Go runs fn in the background and reports true, unless shutdown has
// begun: then fn never runs and Go reports false.
func (w *lifeWork) Go(fn func()) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping {
		return false
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		fn()
	}()
	return true
}

// Stop refuses new work and waits for the work already running, until the
// grace runs out; it reports whether all of it ended. Cancel the context
// that work runs under first, or Stop waits out the grace. Later calls
// share the first call's deadline.
//
// shortcut: work that ignores cancellation (a Windows Recycle Bin move, a
// service's data removal) is abandoned after the grace and dies with the
// process, possibly after the registry closed; give such steps a context
// if one is seen to need finishing.
func (w *lifeWork) Stop() bool {
	w.mu.Lock()
	if !w.stopping {
		w.stopping = true
		grace := w.grace
		if grace == 0 {
			grace = stopGrace
		}
		w.deadline = time.Now().Add(grace)
		w.idle = make(chan struct{})
		go func() {
			w.wg.Wait()
			close(w.idle)
		}()
	}
	idle, deadline := w.idle, w.deadline
	w.mu.Unlock()
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-idle:
		return true
	case <-timer.C:
		return false
	}
}
