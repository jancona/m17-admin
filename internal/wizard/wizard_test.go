package wizard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	s, err := st.Load()
	if err != nil || s.Started || len(s.Done) > 0 {
		t.Fatalf("empty store: %+v, %v", s, err)
	}
	s.Started = true
	s.Draft.WiFiPassword = "secret"
	s.MarkDone("password")
	s.MarkDone("password")
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "wizard.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("wizard.json mode %v, want 0600 (it may hold a WiFi password)", info.Mode().Perm())
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Started || got.Draft.WiFiPassword != "secret" || len(got.Done) != 1 {
		t.Errorf("loaded %+v", got)
	}
	if err := st.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := st.Clear(); err != nil {
		t.Errorf("clearing twice: %v", err)
	}
	if got, _ := st.Load(); got.Started {
		t.Error("state survived Clear")
	}
}

func TestDamagedStateStartsOver(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "wizard.json"), []byte("{not json"), 0o600)
	s, err := NewStore(dir).Load()
	if err != nil || s.Started {
		t.Errorf("damaged file: %+v, %v", s, err)
	}
}

func TestOrder(t *testing.T) {
	var s State
	if !s.Reachable("password") || s.Reachable("network") {
		t.Error("only the first step is reachable at the start")
	}
	if s.FirstOpen() != "password" {
		t.Errorf("FirstOpen = %s", s.FirstOpen())
	}
	s.MarkDone("password")
	if !s.Reachable("network") || s.Reachable("station") {
		t.Error("after password: network reachable, station not")
	}
	for _, st := range Steps {
		s.MarkDone(st.ID)
	}
	if s.FirstOpen() != "review" {
		t.Errorf("all done: FirstOpen = %s, want review", s.FirstOpen())
	}
	if Next("updates") != "ssh" || Next("ssh") != "review" || Next("review") != "" || Prev("password") != "" || Prev("network") != "password" {
		t.Error("Next/Prev")
	}
	if Index("nope") != -1 || s.Reachable("nope") {
		t.Error("unknown step")
	}
}
