package tasks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/d3v0psdan/bench/daemon/internal/api"
	"github.com/d3v0psdan/bench/daemon/internal/binman"
)

func TestEndRecordsOutcome(t *testing.T) {
	tests := []struct {
		name      string
		cancel    bool
		err       error
		wantState string
		wantErr   error
	}{
		{"success", false, nil, api.TaskDone, nil},
		{"failure", false, errors.New("boom"), api.TaskFailed, errors.New("boom")},
		{"cancelled wins over the unwind error", true, errors.New("context canceled"), api.TaskCancelled, ErrCancelled},
		{"work that finished before the cancel landed is done", true, nil, api.TaskDone, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Registry{}
			task := r.Begin("binary.install", "Installing PHP 8.4", "php/8.4", true)
			if tt.cancel {
				if err := r.Cancel(r.List()[0].ID); err != nil {
					t.Fatalf("Cancel: %v", err)
				}
				if task.Context().Err() == nil {
					t.Fatal("cancel didn't end the task context")
				}
			}
			got := task.End(tt.err)
			if fmt.Sprint(got) != fmt.Sprint(tt.wantErr) {
				t.Fatalf("End returned %v, want %v", got, tt.wantErr)
			}
			listed := r.List()[0]
			if listed.State != tt.wantState || listed.Cancellable || listed.EndedAt == nil {
				t.Fatalf("task after End = %+v", listed)
			}
		})
	}
}

func TestCancelRefusals(t *testing.T) {
	r := &Registry{}
	if err := r.Cancel("t99"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	fixed := r.Begin("service.clone", "Cloning", "db", false)
	if err := r.Cancel(r.List()[0].ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("non-cancellable: %v", err)
	}
	_ = fixed.End(nil)
	done := r.Begin("binary.install", "Installing", "php/8.4", true)
	_ = done.End(nil)
	if err := r.Cancel(r.List()[0].ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("finished task: %v", err)
	}
}

func TestUncancellableClosesTheWindow(t *testing.T) {
	r := &Registry{}
	task := r.Begin("service.create", "Creating", "db", true)
	if err := task.Uncancellable(); err != nil {
		t.Fatalf("Uncancellable: %v", err)
	}
	if err := r.Cancel(r.List()[0].ID); !errors.Is(err, ErrNotCancellable) {
		t.Fatalf("cancel after Uncancellable: %v", err)
	}

	late := r.Begin("service.create", "Creating", "db2", true)
	if err := r.Cancel(r.List()[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := late.Uncancellable(); !errors.Is(err, ErrCancelled) {
		t.Fatalf("Uncancellable after a cancel = %v, want ErrCancelled", err)
	}
}

func TestProgressGoesOnlyToWatchers(t *testing.T) {
	r := &Registry{}
	php := r.Begin("binary.install", "PHP", "php/8.4", true)
	php.Watch("php", "8.4.23")
	r.Begin("binary.install", "PHP", "php/8.3", true).Watch("php", "8.3.30")

	r.Progress(binman.Progress{Name: "php", Version: "8.4.23", Phase: "downloading", Received: 10, Total: 100})
	for _, task := range r.List() {
		got := task.Download != nil
		if want := task.Target == "php/8.4"; got != want {
			t.Fatalf("%s has progress = %v, want %v", task.Target, got, want)
		}
	}
	_ = php.End(nil)
	r.Progress(binman.Progress{Name: "php", Version: "8.4.23", Received: 99})
	for _, task := range r.List() {
		if task.Target == "php/8.4" && task.Download.Received != 10 {
			t.Fatal("a finished task must not take new progress")
		}
	}
}

func TestHistoryIsBoundedAndNewestFirst(t *testing.T) {
	r := &Registry{}
	for i := range keepFinished + 5 {
		_ = r.Begin("binary.install", fmt.Sprint(i), "x", true).End(nil)
	}
	running := r.Begin("binary.install", "running", "x", true)
	list := r.List()
	if len(list) != keepFinished+1 {
		t.Fatalf("len = %d, want %d finished + 1 running", len(list), keepFinished)
	}
	if list[0].Title != "running" || list[1].Title != fmt.Sprint(keepFinished+4) {
		t.Fatalf("not newest first: %q, %q", list[0].Title, list[1].Title)
	}
	_ = running.End(nil)
}

func TestOnChangeSeesEveryStep(t *testing.T) {
	var states []string
	r := &Registry{OnChange: func(list []api.Task) { states = append(states, list[0].State+":"+list[0].Phase) }}
	task := r.Begin("binary.install", "PHP", "php/8.4", true)
	task.SetPhase("downloading")
	_ = task.End(nil)
	want := []string{"running:", "running:downloading", "done:"}
	if fmt.Sprint(states) != fmt.Sprint(want) {
		t.Fatalf("OnChange saw %v, want %v", states, want)
	}
}

func TestNilRegistryIsInert(t *testing.T) {
	var r *Registry
	task := r.Begin("setup", "Setup", "setup", false)
	task.SetPhase("x")
	task.Watch("php", "8.4")
	if err := task.Uncancellable(); err != nil {
		t.Fatal(err)
	}
	if err := task.End(errors.New("boom")); err == nil || err.Error() != "boom" {
		t.Fatalf("nil registry must pass errors through, got %v", err)
	}
	if len(r.List()) != 0 || !errors.Is(r.Cancel("t1"), ErrNotFound) {
		t.Fatal("nil registry must list nothing and find nothing")
	}
}
