package tui

import (
	tea "charm.land/bubbletea/v2"
	"strings"
	"testing"
	"zwiftboard/internal/config"
)

func TestInputWarningVisibleAndCleared(t *testing.T) {
	m := New(Config{InputWarning: "Accessibility permission required", Focus: "TextEdit"})
	if got := m.focusAlert(); !strings.Contains(got, "Accessibility permission required") {
		t.Fatalf("input warning hidden: %q", got)
	}
	next, _ := m.Update(InputWarningMsg(""))
	m = next.(Model)
	if got := m.focusAlert(); got != "" {
		t.Fatalf("stale input warning: %q", got)
	}
	next, _ = m.Update(InputWarningMsg("Auto-focus is unsupported on macOS"))
	m = next.(Model)
	if got := m.focusAlert(); !strings.Contains(got, "unsupported") {
		t.Fatal(got)
	}
}

func TestBluetoothSetupUsesPlatformHelp(t *testing.T) {
	m := New(Config{BluetoothHelp: "Allow Bluetooth in Privacy & Security"})
	got := m.View().Content
	if !strings.Contains(got, "Privacy & Security") || strings.Contains(got, "Windows:") {
		t.Fatalf("wrong platform setup guidance: %s", got)
	}
}

// Profile and mode updates must not erase an independently reported permission failure.
func TestInputWarningSurvivesProfileAndModeChanges(t *testing.T) {
	m := New(Config{InputWarning: "Accessibility permission required"})
	for _, msg := range []tea.Msg{ProfileMsg{Name: "ride"}, ModeMsg{Mode: config.ModeSingle}} {
		next, _ := m.Update(msg)
		m = next.(Model)
		if !strings.Contains(m.focusAlert(), "Accessibility permission required") {
			t.Fatal("profile/mode change lost permission warning")
		}
	}
}

func TestInputWarningAfterRecovery(t *testing.T) {
	cfg := Config{RecoveryPath: "broken.yml", RecoveryStart: func(string) (Config, bool) {
		return Config{InputWarning: "Accessibility permission required", BluetoothHelp: "macOS Bluetooth permission"}, true
	}}
	next, _ := New(cfg).Update(key("y"))
	m := next.(Model)
	if !strings.Contains(m.focusAlert(), "Accessibility permission required") {
		t.Fatal("recovery lost permission warning")
	}
	if !strings.Contains(m.View().Content, "macOS Bluetooth permission") {
		t.Fatal("recovery lost Bluetooth guidance")
	}
}
