package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpsertDeletePersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	route, created, err := st.Upsert("5.5.2.13", "192.168.100.254")
	if err != nil || !created {
		t.Fatalf("upsert: %v created=%v", err, created)
	}
	if route.Host != "5.5.2.13" || route.Gateway != "192.168.100.254" {
		t.Fatalf("unexpected route: %+v", route)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("5.5.2.13"); !ok {
		t.Fatal("route 没有持久化")
	}

	if _, created, err := reloaded.Upsert("5.5.2.13", "10.0.0.1"); err != nil || created {
		t.Fatalf("second upsert: %v created=%v", err, created)
	}

	if err := reloaded.Delete("5.5.2.13"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("5.5.2.13"); ok {
		t.Fatal("route 应已删除")
	}
	if err := reloaded.Delete("5.5.2.13"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNewSkipsInvalidRoutes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "routes.json")
	body := `[
  {"host": "5.5.2.13", "gateway": "192.168.100.254"},
  {"host": "bad", "gateway": "192.168.100.254"},
  {"host": "1.2.3.4", "gateway": "1.2.3.4"}
]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(st.Routes()); got != 1 {
		t.Fatalf("expected 1 route, got %d", got)
	}
}
