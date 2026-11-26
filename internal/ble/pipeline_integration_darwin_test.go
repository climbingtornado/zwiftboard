//go:build darwin && cgo && integration

package ble

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"zwiftboard/internal/config"
	"zwiftboard/internal/keys"
)

// These fixtures are actual 0x23 protobuf bitmap frames: B pressed/released.
var integrationPress = []byte{0x23, 0x08, 0xDF, 0xFF, 0xFF, 0xFF, 0x0F}
var integrationIdle = []byte{0x23, 0x08, 0xFF, 0xFF, 0xFF, 0xFF, 0x0F}

type pipelineHarness struct {
	events     []keys.IntegrationEvent
	timers     []func()
	delays     []time.Duration
	km         *KeyMap
	configPath string
	receive    [2]func([]byte)
	close      [2]func()
}

func newPipeline(t *testing.T) *pipelineHarness {
	t.Helper()
	h := &pipelineHarness{}
	path := filepath.Join(t.TempDir(), "config.yml")
	h.configPath = path
	if err := os.WriteFile(path, []byte("defaultProfile: ride\nprofiles:\n  ride:\n    B: i\n  other:\n    B: k\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path, "")
	if err != nil {
		t.Fatal(err)
	}
	h.km = NewKeyMap(cfg.Mapping(config.ModeDouble))
	restore := keys.InstallIntegrationKeyboard(func(e keys.IntegrationEvent) bool {
		h.events = append(h.events, e)
		return true
	}, func(d time.Duration, f func()) func() {
		h.delays = append(h.delays, d)
		h.timers = append(h.timers, f)
		// Deliberately retain cancelled callbacks to exercise stale timer protection.
		return func() {}
	})
	t.Cleanup(restore)
	keys.SetWindowTarget("")
	for i := range h.receive {
		holds := holdKeys(keys.Down, keys.Up)
		gate := &notificationGate{}
		handler := ButtonHandler("integration", h.km, holds.Press, holds.Release)
		h.receive[i] = gate.Wrap(asyncNotify(func([]byte) {}, func() {}, handler))
		h.close[i] = func() { gate.Close(); holds.ReleaseAll() }
		t.Cleanup(h.close[i])
	}
	t.Cleanup(func() { keys.SetWindowTarget("") })
	return h
}

func (h *pipelineHarness) pair(frame []byte) {
	for _, receive := range h.receive {
		receive(frame)
	}
}
func (h *pipelineHarness) fire(t *testing.T) {
	t.Helper()
	if len(h.timers) == 0 {
		t.Fatal("held input did not schedule a repeat")
	}
	f := h.timers[0]
	h.timers = h.timers[1:]
	f()
}
func (h *pipelineHarness) want(t *testing.T, events ...keys.IntegrationEvent) {
	t.Helper()
	if !reflect.DeepEqual(h.events, events) {
		t.Fatalf("Quartz output = %+v, want %+v", h.events, events)
	}
}

func TestIntegrationMirroredFramesHoldAndRelease(t *testing.T) {
	h := newPipeline(t)
	h.pair([]byte{0x23, 0x08}) // truncated protobuf must not press a key
	h.pair(integrationPress)
	h.pair(integrationPress)                               // retransmissions must not restart the hold
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}) // US physical I
	h.fire(t)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34, Down: true, Repeat: true})
	if !reflect.DeepEqual(h.delays, []time.Duration{250 * time.Millisecond, 33 * time.Millisecond}) {
		t.Fatalf("repeat schedule = %v", h.delays)
	}
	h.receive[0](integrationIdle)
	if len(h.events) != 2 {
		t.Fatal("first mirrored release lifted sibling's held key")
	}
	h.receive[1](integrationIdle)
	h.fire(t) // already queued timer must not emit after final release
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34, Down: true, Repeat: true}, keys.IntegrationEvent{Code: 34})
}

func TestIntegrationDisconnectRejectsLateFrames(t *testing.T) {
	h := newPipeline(t)
	h.pair(integrationPress)
	h.close[0]()
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true})
	h.close[1]()
	h.pair(integrationIdle)
	h.pair(integrationPress) // queued notifications after teardown cannot acquire keys
	h.fire(t)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
}

func TestIntegrationProfileSwapReleasesOldMapping(t *testing.T) {
	h := newPipeline(t)
	h.pair(integrationPress)
	// Same order as the TUI profile switch: release, swap, retarget.
	cfg, err := config.Load(h.configPath, "other")
	if err != nil {
		t.Fatal(err)
	}
	ReleaseAllHeld()
	h.km.Swap(cfg.Mapping(config.ModeDouble))
	keys.SetWindowTarget("")
	h.fire(t)
	h.pair(integrationIdle)
	h.pair(integrationPress)
	h.pair(integrationIdle)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34}, keys.IntegrationEvent{Code: 40, Down: true}, keys.IntegrationEvent{Code: 40})
}

func TestIntegrationShutdownDuringHold(t *testing.T) {
	h := newPipeline(t)
	h.pair(integrationPress)
	keys.Shutdown()
	h.fire(t)
	h.pair(integrationIdle)
	h.pair(integrationPress)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
}

func TestIntegrationBlockedFocusCanRecover(t *testing.T) {
	h := newPipeline(t)
	keys.SetWindowTarget("MyWhoosh")
	h.pair(integrationPress)
	if len(h.events) != 0 {
		t.Fatal("unsupported focus target emitted input")
	}
	h.pair(integrationIdle)
	keys.SetWindowTarget("")
	h.pair(integrationPress)
	h.pair(integrationIdle)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
}

// The left pod owns LEFT and mirrors B. Both map to I; neither release alone
// may lift I while the other button or the sibling session still owns it.
func TestIntegrationDualSharedKeyReleaseOrders(t *testing.T) {
	for _, releaseLeftFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(releaseLeftFirst), func(t *testing.T) {
			h := newPipeline(t)
			binding, _ := keys.Resolve("i")
			h.km.Swap(map[string]keys.Binding{"LEFT": binding, "B": binding})
			both := []byte{0x23, 0x08, 0xde, 0xff, 0xff, 0xff, 0x0f}
			leftOnly := []byte{0x23, 0x08, 0xfe, 0xff, 0xff, 0xff, 0x0f}
			h.receive[0](both)
			h.receive[1](integrationPress)
			if releaseLeftFirst {
				h.receive[0](integrationPress)
			} else {
				h.receive[0](leftOnly)
			}
			h.close[1]()
			h.want(t, keys.IntegrationEvent{Code: 34, Down: true})
			h.receive[0](integrationIdle)
			h.fire(t)
			h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
		})
	}
}

func TestIntegrationControllerModesWithNestedAndLegacyProfiles(t *testing.T) {
	const flat = "    LEFT: a\n    UP: b\n    RIGHT: c\n    DOWN: d\n    MIN: e\n    A: f\n    B: g\n    Y: h\n    Z: i\n    PLUS: j\n"
	const nested = "    single:\n      A: f\n      B: g\n      Y: h\n      Z: i\n      PLUS: j\n    double:\n      LEFT: a\n      UP: b\n      RIGHT: c\n      DOWN: d\n      MIN: e\n      A: f\n      B: g\n      Y: h\n      Z: i\n      PLUS: j\n"
	buttons := []struct {
		bit  uint32
		code uint16
		left bool
	}{
		{1, 0, true}, {2, 11, true}, {4, 8, true}, {8, 2, true}, {256, 14, true},
		{16, 3, false}, {32, 5, false}, {64, 4, false}, {128, 34, false}, {4096, 38, false},
	}
	for _, fixture := range []struct{ name, body string }{{"legacy", flat}, {"nested", nested}} {
		for _, mode := range []config.Mode{config.ModeSingle, config.ModeDouble} {
			t.Run(fixture.name+"/"+string(mode), func(t *testing.T) {
				h := newPipeline(t)
				if err := os.WriteFile(h.configPath, []byte("defaultProfile: ride\nprofiles:\n  ride:\n"+fixture.body), 0600); err != nil {
					t.Fatal(err)
				}
				cfg, err := config.Load(h.configPath, "")
				if err != nil {
					t.Fatal(err)
				}
				h.km.Swap(cfg.Mapping(mode))
				var want []keys.IntegrationEvent
				for _, button := range buttons {
					frame := []byte{0x23, 0x08}
					frame = binary.AppendUvarint(frame, uint64(^button.bit))
					h.receive[0](frame)
					h.receive[0](integrationIdle)
					if mode == config.ModeDouble || !button.left {
						want = append(want, keys.IntegrationEvent{Code: button.code, Down: true}, keys.IntegrationEvent{Code: button.code})
					}
					h.want(t, want...)
				}
			})
		}
	}
}

func TestIntegrationOldSessionCannotReleaseNewHoldAfterReset(t *testing.T) {
	for _, disconnect := range []bool{true, false} {
		t.Run(fmt.Sprint(disconnect), func(t *testing.T) {
			h := newPipeline(t)
			h.receive[0](integrationPress)
			ReleaseAllHeld()
			h.receive[1](integrationPress)
			if disconnect {
				h.close[0]()
			} else {
				h.receive[0](integrationIdle)
			}
			h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34}, keys.IntegrationEvent{Code: 34, Down: true})
			h.receive[1](integrationIdle)
			h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34}, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
		})
	}
}

func TestIntegrationOldButtonReleaseCannotConsumeNewButtonHold(t *testing.T) {
	h := newPipeline(t)
	binding, _ := keys.Resolve("i")
	h.km.Swap(map[string]keys.Binding{"LEFT": binding, "B": binding})
	left := []byte{0x23, 0x08, 0xfe, 0xff, 0xff, 0xff, 0x0f}
	both := []byte{0x23, 0x08, 0xde, 0xff, 0xff, 0xff, 0x0f}
	h.receive[0](left)
	ReleaseAllHeld()
	h.receive[0](both)
	h.receive[0](integrationPress)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34}, keys.IntegrationEvent{Code: 34, Down: true})
	h.receive[0](integrationIdle)
	h.want(t, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34}, keys.IntegrationEvent{Code: 34, Down: true}, keys.IntegrationEvent{Code: 34})
}

func TestIntegrationRejectedPressCannotReleaseAcceptedSharedHold(t *testing.T) {
	var events []keys.IntegrationEvent
	allowed := false
	restore := keys.InstallIntegrationKeyboard(func(e keys.IntegrationEvent) bool {
		events = append(events, e)
		return true
	}, func(time.Duration, func()) func() { return func() {} }, func() bool { return allowed })
	t.Cleanup(restore)
	keys.SetWindowTarget("")
	b, _ := keys.Resolve("i")
	holds := holdKeys(keys.Down, keys.Up)
	t.Cleanup(holds.ReleaseAll)
	receive := ButtonHandler("permission recovery", NewKeyMap(map[string]keys.Binding{"LEFT": b, "B": b}), holds.Press, holds.Release)
	receive([]byte{0x23, 0x08, 0xfe, 0xff, 0xff, 0xff, 0x0f}) // LEFT rejected
	allowed = true
	receive([]byte{0x23, 0x08, 0xde, 0xff, 0xff, 0xff, 0x0f}) // B accepted
	receive(integrationPress)                                 // old LEFT release must not lift B
	if !reflect.DeepEqual(events, []keys.IntegrationEvent{{Code: 34, Down: true}}) {
		t.Fatalf("rejected press consumed accepted hold: %+v", events)
	}
	receive(integrationIdle)
	if !reflect.DeepEqual(events, []keys.IntegrationEvent{{Code: 34, Down: true}, {Code: 34}}) {
		t.Fatalf("final release: %+v", events)
	}
}
