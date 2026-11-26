package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"zwiftboard/internal/config"
)

func TestStartupSelectionSharedByPlainAndTUI(t *testing.T) {
	for _, saved := range []string{"single", "double", ""} {
		t.Run(saved, func(t *testing.T) {
			data := "defaultProfile: ride\nprofiles:\n  ride:\n    single:\n      B: i\n    double:\n      LEFT: left\n      B: k\n"
			if saved != "" {
				data = "defaultMode: " + saved + "\n" + data
			}
			path := writeProfileConfig(t, data)
			cfg, err := config.Load(path, "")
			if err != nil {
				t.Fatal(err)
			}
			state := newProfileState(path, cfg)
			wantMode := config.ModeDouble
			if saved == "single" {
				wantMode = config.ModeSingle
			}
			want := cfg.Mapping(wantMode)
			if state.mode != wantMode {
				t.Fatalf("startup mode=%s", state.mode)
			}
			for _, name := range []string{"LEFT", "B"} {
				b, ok := state.km.Get(name)
				w, exists := want[name]
				if ok != exists || b != w {
					t.Fatalf("plain/TUI shared mapping %s=%+v, %v", name, b, ok)
				}
			}
			if !reflect.DeepEqual(*state.vkIdx.Load(), vkIndex(want)) {
				t.Fatal("startup key index uses wrong mode")
			}
			screen := tuiConfig(cfg, true, state.mode, nil, path)
			if screen.Mode != wantMode || !reflect.DeepEqual(screen.Bindings, bindingTokens(want)) {
				t.Fatal("screen differs from input mapping")
			}
			unchanged, _ := os.ReadFile(path)
			if string(unchanged) != data {
				t.Fatal("startup rewrote user config")
			}
		})
	}
}

func TestSelectionSurvivesRestart(t *testing.T) {
	path := writeProfileConfig(t, "defaultProfile: a\ndefaultMode: single\nprofiles:\n  a:\n    B: i\n  b:\n    B: k\n")
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	s := newProfileState(path, cfg)
	s.switchBy(1)
	s.toggleMode()
	reloaded, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	restarted := newProfileState(path, reloaded)
	if restarted.cur.Profile != "b" || restarted.mode != config.ModeDouble {
		t.Fatalf("restart=%s/%s", restarted.cur.Profile, restarted.mode)
	}
	b, _ := restarted.km.Get("B")
	if b.Token != "k" {
		t.Fatalf("restarted binding=%+v", b)
	}
}

func TestPersistenceFailureKeepsLiveMode(t *testing.T) {
	path := writeProfileConfig(t, "defaultProfile: ride\ndefaultMode: single\nprofiles:\n  ride:\n    single:\n      B: i\n    double:\n      B: k\n")
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	s := newProfileState(path, cfg)
	// A missing parent makes persistence fail without relying on user privileges.
	s.path = filepath.Join(t.TempDir(), "missing", "config.yml")
	s.toggleMode()
	b, _ := s.km.Get("B")
	if s.mode != config.ModeDouble || b.Token != "k" {
		t.Fatal("persistence failure rolled back live selection")
	}
	original, err := config.Load(path, "")
	if err != nil || original.DefaultMode != config.ModeSingle {
		t.Fatal("unrelated config changed")
	}
}
