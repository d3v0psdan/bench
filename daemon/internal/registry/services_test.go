package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTest(t *testing.T) *Registry {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "bench.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestServiceRoundTrip(t *testing.T) {
	r := openTest(t)
	in := Service{Kind: "mysql", Channel: "8.4", Name: "app-db", Port: 3306,
		BinaryDir: "/b", DataDir: "/d", Config: `{"x":1}`, Autostart: true}
	if err := r.InsertService(in); err != nil {
		t.Fatal(err)
	}
	got, err := r.Service("app-db")
	if err != nil {
		t.Fatal(err)
	}
	in.ID = got.ID
	if got != in {
		t.Fatalf("got %+v, want %+v", got, in)
	}
	if err := r.SetServiceAutostart("app-db", false); err != nil {
		t.Fatal(err)
	}
	if err := r.SetServiceBinaryDir("app-db", "/b2"); err != nil {
		t.Fatal(err)
	}
	list, err := r.Services()
	if err != nil || len(list) != 1 || list[0].Autostart || list[0].BinaryDir != "/b2" {
		t.Fatalf("after updates: %+v, %v", list, err)
	}
	if err := r.DeleteService("app-db"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Service("app-db"); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("want ErrServiceNotFound after delete, got %v", err)
	}
}

func TestInsertServiceDuplicateName(t *testing.T) {
	r := openTest(t)
	s := Service{Kind: "redis", Channel: "9.1", Name: "cache"}
	if err := r.InsertService(s); err != nil {
		t.Fatal(err)
	}
	if err := r.InsertService(s); !errors.Is(err, ErrServiceExists) {
		t.Fatalf("want ErrServiceExists, got %v", err)
	}
}

func TestUpdateMissingService(t *testing.T) {
	r := openTest(t)
	if err := r.SetServiceAutostart("nope", true); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("want ErrServiceNotFound, got %v", err)
	}
}
