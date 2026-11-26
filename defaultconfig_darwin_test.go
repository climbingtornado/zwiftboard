//go:build darwin

package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"zwiftboard/internal/config"
)

func TestMacFirstRunUsesForegroundKeyboardProfile(t *testing.T) {
	old := legacyConfigPaths
	legacyConfigPaths = func() []string { return nil }
	t.Cleanup(func() { legacyConfigPaths = old })
	path := filepath.Join(t.TempDir(), "config.yml")
	ensureConfig(path)
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profile != "keyboard" || cfg.DefaultMode != config.ModeSingle || cfg.StartProgram != "" || cfg.FocusProgramNamePrefixOnClick != "" || len(cfg.Single) != 5 || len(cfg.Double) != 10 {
		t.Fatalf("first-run config is not usable on macOS: %+v", cfg)
	}
}

// Generated configs must allow every bundled profile to emit foreground input.
// Keeping a Windows focus target would block the macOS key backend.
func TestMacGeneratedProfilesClearWindowsOptions(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-run", true: "recovery"}[recovery], func(t *testing.T) {
			old := legacyConfigPaths
			legacyConfigPaths = func() []string { return nil }
			t.Cleanup(func() { legacyConfigPaths = old })
			path := filepath.Join(t.TempDir(), "config.yml")
			if recovery {
				writeFile(t, path, "profiles: [broken")
				cfg, ok := recoverConfig(path)
				if !ok || cfg.Profile != "keyboard" || cfg.DefaultMode != config.ModeSingle {
					t.Fatalf("recovery = %+v, %v", cfg, ok)
				}
			} else if !ensureConfig(path) {
				t.Fatal("first-run creation failed")
			}
			names, err := config.ProfileList(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range names {
				cfg, err := config.Load(path, name)
				if err != nil {
					t.Fatal(err)
				}
				if cfg.StartProgram != "" || cfg.FocusProgramNamePrefixOnClick != "" {
					t.Errorf("profile %s retains Windows options: %+v", name, cfg)
				}
				source, err := config.Load(writeDefaultConfig(t), name)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(cfg.Single, source.Single) || !reflect.DeepEqual(cfg.Double, source.Double) {
					t.Errorf("profile %s bindings changed during generation", name)
				}
			}
		})
	}
}

func TestMacExistingAndMigratedOptionsRemainUnchanged(t *testing.T) {
	const custom = "defaultProfile: custom\nprofiles:\n  custom:\n    focusProgramNamePrefixOnClick: MyWhoosh\n    startProgram: custom.exe\n    A: a\n    LEFT: left\n"
	for _, migrate := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "legacy"}[migrate], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yml")
			legacy := filepath.Join(t.TempDir(), "config.yaml")
			old := legacyConfigPaths
			legacyConfigPaths = func() []string { return []string{legacy} }
			t.Cleanup(func() { legacyConfigPaths = old })
			if migrate {
				writeFile(t, legacy, custom)
			} else {
				writeFile(t, path, custom)
			}
			ensureConfig(path)
			if got := readFile(t, path); got != custom {
				t.Fatalf("user config rewritten: %q", got)
			}
			cfg, err := config.Load(path, "")
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Single["A"].Token != "a" || cfg.Double["LEFT"].Token != "left" || len(cfg.Single) != 1 {
				t.Fatalf("legacy mode conversion incorrect: %+v", cfg)
			}
		})
	}
}
