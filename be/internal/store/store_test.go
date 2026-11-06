package store

import (
	"os"
	"path/filepath"
	"testing"

	"be/internal/model"
)

func TestUpsertDeletePersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")

	st, err := New(path, model.Settings{AutoHeal: true, IntervalSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Routes()) != 0 {
		t.Fatal("expected empty store")
	}

	r, created, err := st.Upsert("5.5.2.13", "192.168.100.254")
	if err != nil || !created {
		t.Fatalf("upsert: %v created=%v", err, created)
	}
	if r.Host != "5.5.2.13" || r.Gateway != "192.168.100.254" {
		t.Fatalf("unexpected route: %+v", r)
	}

	// Re-open from disk and verify persistence.
	reloaded, err := New(path, model.Settings{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("5.5.2.13")
	if !ok {
		t.Fatal("route was not persisted")
	}
	if got.Gateway != "192.168.100.254" {
		t.Fatalf("unexpected gateway after reload: %s", got.Gateway)
	}
	if s := reloaded.Settings(); !s.AutoHeal || s.IntervalSeconds != 5 {
		t.Fatalf("unexpected settings after reload: %+v", s)
	}

	// Update keeps CreatedAt and reports created=false.
	updated, created, err := reloaded.Upsert("5.5.2.13", "10.0.0.1")
	if err != nil || created {
		t.Fatalf("update: %v created=%v", err, created)
	}
	if !updated.CreatedAt.Equal(got.CreatedAt) {
		t.Fatal("CreatedAt should not change on update")
	}

	if _, err := reloaded.Delete("5.5.2.13"); err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("5.5.2.13"); ok {
		t.Fatal("route should be deleted")
	}
	if _, err := reloaded.Delete("5.5.2.13"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestNewSkipsInvalidRoutes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routes.json")
	body := `{
  "settings": {"autoHeal": false, "intervalSeconds": 7},
  "routes": [
    {"host": "5.5.2.13", "gateway": "192.168.100.254"},
    {"host": "bad", "gateway": "192.168.100.254"},
    {"host": "1.2.3.4", "gateway": "1.2.3.4"}
  ]
}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := New(path, model.Settings{AutoHeal: true, IntervalSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(st.Routes()); got != 1 {
		t.Fatalf("expected 1 valid route, got %d", got)
	}
	if s := st.Settings(); s.AutoHeal || s.IntervalSeconds != 7 {
		t.Fatalf("settings not applied: %+v", s)
	}
}

func TestSettingsNormalized(t *testing.T) {
	st, err := New("", model.Settings{AutoHeal: true, IntervalSeconds: 0})
	if err != nil {
		t.Fatal(err)
	}
	if s := st.Settings(); s.IntervalSeconds != 5 {
		t.Fatalf("expected normalized interval 5, got %d", s.IntervalSeconds)
	}
	if err := st.SetSettings(model.Settings{AutoHeal: false, IntervalSeconds: 99999}); err != nil {
		t.Fatal(err)
	}
	if s := st.Settings(); s.IntervalSeconds != 3600 {
		t.Fatalf("expected clamped interval 3600, got %d", s.IntervalSeconds)
	}
}
