package wizard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestStaleStateStartsOver(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	now := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return now }
	s := State{Started: true}
	s.Draft.HATType = "sx1255"
	s.MarkDone("password")
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}

	now = now.Add(23 * time.Hour)
	if got, _ := st.Load(); !got.Started {
		t.Error("progress from 23 hours ago was dropped")
	}
	now = now.Add(2 * time.Hour)
	if got, _ := st.Load(); got.Started {
		t.Error("progress from 25 hours ago was resumed")
	}
	now = now.Add(-72 * time.Hour) // the clock went back
	if got, _ := st.Load(); !got.Started {
		t.Error("progress saved \"in the future\" was dropped")
	}

	// Saved by a wizard with other steps, or by one from before steps were recorded
	now = time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	for _, steps := range [][]string{{"password", "network", "review"}, nil} {
		b, _ := json.Marshal(State{Started: true, Done: []string{"password", "network"}, Steps: steps, Saved: now})
		os.WriteFile(filepath.Join(dir, "wizard.json"), b, 0o600)
		if got, _ := st.Load(); got.Started {
			t.Errorf("progress saved with steps %v was resumed", steps)
		}
	}
}
