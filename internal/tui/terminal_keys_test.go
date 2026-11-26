package tui

import (
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// Exercise the actual terminal decoder: synthetic Ctrl+M messages hide its
// collision with carriage return in terminals without enhanced key reporting.
func TestLegacyTerminalModeShortcut(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		toggle      bool
	}{
		{"ctrl-g", "\x07", runtime.GOOS == "darwin"},
		{"ctrl-m-is-enter", "\r", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoder := uv.EventDecoder{}
			n, event := decoder.Decode([]byte(tc.input))
			key, ok := event.(uv.KeyPressEvent)
			if n != 1 || !ok {
				t.Fatalf("decoded %d bytes: %T", n, event)
			}
			calls := 0
			cfg := testCfg()
			cfg.ToggleMode = func() { calls++ }
			_, cmd := New(cfg).Update(tea.KeyPressMsg(key))
			if cmd != nil {
				cmd()
			}
			want := 0
			if tc.toggle {
				want = 1
			}
			if calls != want {
				t.Fatalf("decoded %q: toggle calls=%d, want %d", key.String(), calls, want)
			}
		})
	}
}

func TestModeShortcutDoesNotEscapeDialogs(t *testing.T) {
	for _, dialog := range []string{"login", "recovery"} {
		t.Run(dialog, func(t *testing.T) {
			calls := 0
			cfg := testCfg()
			cfg.ToggleMode = func() { calls++ }
			m := New(cfg)
			if dialog == "login" {
				m.auth.state = AuthPrompt
			} else {
				m.recovery.Active = true
			}
			_, cmd := m.Update(ctrlKey(toggleModeKey[len(toggleModeKey)-1:]))
			if cmd != nil {
				cmd()
			}
			if calls != 0 {
				t.Fatal("mode shortcut escaped dialog")
			}
		})
	}
}

func TestPlatformModeShortcuts(t *testing.T) {
	old := toggleModeKey
	t.Cleanup(func() { toggleModeKey = old })
	for _, platform := range []string{"darwin", "windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			want := "ctrl+m"
			if platform == "darwin" {
				want = "ctrl+g"
			}
			toggleModeKey = modeShortcut(platform)
			if toggleModeKey != want {
				t.Fatalf("shortcut=%s, want %s", toggleModeKey, want)
			}
			for _, chord := range []string{"g", "m"} {
				calls := 0
				cfg := testCfg()
				cfg.ToggleMode = func() { calls++ }
				_, cmd := New(cfg).Update(ctrlKey(chord))
				if cmd != nil {
					cmd()
				}
				expected := 0
				if chord == "m" || platform == "darwin" {
					expected = 1
				}
				if calls != expected {
					t.Fatalf("ctrl+%s: calls=%d, want %d", chord, calls, expected)
				}
			}
			cfg := testCfg()
			cfg.ToggleMode = func() {}
			has(t, on(cfg, PodConnected, PodConnected).View(), want+" mode")
		})
	}
}
