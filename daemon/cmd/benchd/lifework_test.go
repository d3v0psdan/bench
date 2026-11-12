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
