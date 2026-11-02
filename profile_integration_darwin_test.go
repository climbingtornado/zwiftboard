//go:build darwin && cgo && integration

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"zwiftboard/internal/ble"
	"zwiftboard/internal/config"
	"zwiftboard/internal/keys"
)

// Exercise the actual UI callbacks with real decoding, shared holds and Quartz
// state. Only OS event delivery and the repeat clock are replaced.
func TestIntegrationProfileActionsReleaseHeldKeys(t *testing.T) {
	for _, action := range []string{"mode", "profile", "reload", "recovery"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			yaml := "defaultProfile: ride\nprofiles:\n  ride:\n    single:\n      B: k\n    double:\n      B: i\n  other:\n    B: l\n"
			if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path, "")
			if err != nil {
				t.Fatal(err)
			}
			var events []keys.IntegrationEvent
			var timers []func()
			restore := keys.InstallIntegrationKeyboard(func(e keys.IntegrationEvent) bool { events = append(events, e); return true }, func(_ time.Duration, f func()) func() { timers = append(timers, f); return func() {} })
			t.Cleanup(restore)
			oldTarget := keys.CurrentWindowTarget()
			keys.SetWindowTarget("")
			t.Cleanup(func() { keys.SetWindowTarget(oldTarget) })
			km := ble.NewKeyMap(cfg.Double)
			s := &profileState{path: path, names: []string{"ride", "other"}, cur: cfg, mode: config.ModeDouble, km: km}
			holds := ble.NewIntegrationHolds()
			t.Cleanup(holds.ReleaseAll)
			receive := ble.ButtonHandler("profile integration", km, holds.Press, holds.Release)
			press := []byte{0x23, 0x08, 0xDF, 0xFF, 0xFF, 0xFF, 0x0F}
			idle := []byte{0x23, 0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}
			receive(press)
			expectedCode := uint16(40) // K after mode/reload, L after profile/recovery
			switch action {
			case "mode":
				s.toggleMode()
			case "profile":
				s.switchBy(1)
				expectedCode = 37
			case "reload":
				if err := os.WriteFile(path, []byte(strings.ReplaceAll(yaml, "B: i", "B: k")), 0600); err != nil {
					t.Fatal(err)
				}
				s.reload()
			case "recovery":
				next, err := config.Load(path, "other")
				if err != nil {
					t.Fatal(err)
				}
				s.reset(next)
				expectedCode = 37
			}
			if len(timers) != 1 {
				t.Fatalf("repeat callbacks=%d, want one", len(timers))
			}
			timers[0]()
			want := []keys.IntegrationEvent{{Code: 34, Down: true}, {Code: 34}}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("%s failed to release old hold: %+v", action, events)
			}
			receive(idle)
			receive(press)
			receive(idle)
			want = append(want, keys.IntegrationEvent{Code: expectedCode, Down: true}, keys.IntegrationEvent{Code: expectedCode})
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("new mapping events=%+v, want %+v", events, want)
			}
		})
	}
}

func TestIntegrationTUIConfigReportsMacFocusBlockAfterRecovery(t *testing.T) {
	restore := keys.InstallIntegrationKeyboard(func(keys.IntegrationEvent) bool { return true }, func(time.Duration, func()) func() { return func() {} })
	defer restore()
	old := keys.CurrentWindowTarget()
	defer keys.SetWindowTarget(old)
	next := config.Config{Profile: "ride", FocusProgramNamePrefixOnClick: "MyWhoosh.exe"}
	state := &profileState{mode: config.ModeDouble, km: ble.NewKeyMap(nil)}
	state.reset(next)
	screen := tuiConfig(state.current(), false, nil, "config.yml")
	if !strings.Contains(screen.InputWarning, "unsupported") {
		t.Fatalf("configured focus block absent: %q", screen.InputWarning)
	}
	if screen.Mode != config.ModeDouble {
		t.Fatalf("startup mode=%q", screen.Mode)
	}
}

// A released old button must not consume a different button's new reservation
// when both map to the same VK after a live configuration change.
func TestIntegrationProfileActionsIgnoreOldButtonRelease(t *testing.T) {
	for _, action := range []string{"mode", "reload"} {
		t.Run(action, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			data := "defaultProfile: ride\nprofiles:\n  ride:\n    single:\n      B: i\n    double:\n      LEFT: i\n      B: i\n"
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path, "")
			if err != nil {
				t.Fatal(err)
			}
			var events []keys.IntegrationEvent
			restore := keys.InstallIntegrationKeyboard(func(e keys.IntegrationEvent) bool { events = append(events, e); return true }, func(time.Duration, func()) func() { return func() {} })
			t.Cleanup(restore)
			old := keys.CurrentWindowTarget()
			keys.SetWindowTarget("")
			t.Cleanup(func() { keys.SetWindowTarget(old) })
			km := ble.NewKeyMap(cfg.Double)
			s := &profileState{path: path, names: []string{"ride"}, cur: cfg, mode: config.ModeDouble, km: km}
			holds := ble.NewIntegrationHolds()
			t.Cleanup(holds.ReleaseAll)
			receive := ble.ButtonHandler("old release integration", km, holds.Press, holds.Release)
			receive([]byte{0x23, 0x08, 0xFE, 0xFF, 0xFF, 0xFF, 0x0F}) // LEFT
			if action == "mode" {
				s.toggleMode()
				s.toggleMode()
			} else {
				s.reload()
			}
			receive([]byte{0x23, 0x08, 0xDE, 0xFF, 0xFF, 0xFF, 0x0F}) // LEFT and B
			receive([]byte{0x23, 0x08, 0xDF, 0xFF, 0xFF, 0xFF, 0x0F}) // B only
			want := []keys.IntegrationEvent{{Code: 34, Down: true}, {Code: 34}, {Code: 34, Down: true}}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("old LEFT release lifted newly held B: got %+v, want %+v", events, want)
			}
			receive([]byte{0x23, 0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}) // final B release
			want = append(want, keys.IntegrationEvent{Code: 34})
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("final B release: got %+v, want %+v", events, want)
			}
		})
	}
}
