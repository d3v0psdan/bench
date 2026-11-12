package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestLifeWorkStopWaitsForRunningWorkThenRefusesMore(t *testing.T) {
	var w lifeWork
	started := make(chan struct{})
	release := make(chan struct{})
	var finished atomic.Bool
	w.Go(func() {
		close(started)
		<-release
		finished.Store(true)
	})
	<-started

	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned while work was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-stopped
	if !finished.Load() {
		t.Fatal("Stop returned before the work finished")
	}

	var ranLate atomic.Bool
	w.Go(func() { ranLate.Store(true) })
	w.Stop()
	if ranLate.Load() {
		t.Fatal("work started after Stop ran")
	}
}

func TestLifeWorkStopGivesUpOnWorkStillRunningAfterTheGrace(t *testing.T) {
	w := lifeWork{grace: 50 * time.Millisecond}
	release := make(chan struct{})
	defer close(release)
	w.Go(func() { <-release }) // ignores cancellation

	stopped := make(chan bool, 1)
	go func() { stopped <- w.Stop() }()
	select {
	case finished := <-stopped:
		if finished {
			t.Fatal("Stop reported all work finished while some was still running")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop still waiting long after its grace")
	}
}

func TestLifeWorkSecondStopDoesNotWaitAnotherGrace(t *testing.T) {
	const grace = 200 * time.Millisecond
	w := lifeWork{grace: grace}
	release := make(chan struct{})
	defer close(release)
	w.Go(func() { <-release })

	began := time.Now()
	w.Stop()
	w.Stop() // the deferred Stop in run, after the explicit one
	if took := time.Since(began); took > grace*3/2 {
		t.Fatalf("two Stops took %s, want about one grace (%s)", took, grace)
	}
}

func TestLifeWorkGoAfterStopReportsTheRefusal(t *testing.T) {
	var w lifeWork
	w.Stop()
	if w.Go(func() {}) {
		t.Fatal("Go reported it accepted work after Stop")
	}
}
