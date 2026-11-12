package main

import "sync"

// lifeWork runs background work for the daemon's lifetime and lets
// shutdown wait for it, so what that work uses (the registry, the
// supervisor) isn't closed under it.
type lifeWork struct {
	mu       sync.Mutex
	stopping bool
	wg       sync.WaitGroup
}

// Go runs fn in the background, unless shutdown has begun.
func (w *lifeWork) Go(fn func()) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopping {
		return
	}
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		fn()
	}()
}

// Stop refuses new work and waits for the work already running. Cancel
// the context that work runs under first, or Stop waits for it to finish.
func (w *lifeWork) Stop() {
	w.mu.Lock()
	w.stopping = true
	w.mu.Unlock()
	w.wg.Wait()
}
