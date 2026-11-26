package tui

import (
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
		{"ctrl-g", "\x07", true},
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
			_, cmd := m.Update(ctrlKey("g"))
			if cmd != nil {
				cmd()
			}
			if calls != 0 {
				t.Fatal("mode shortcut escaped dialog")
			}
		})
	}
}
