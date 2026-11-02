package ble

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zwiftboard/internal/zwift"
)

type fakeSolver struct {
	reply []byte
	err   error
	got   []byte
}

func (f *fakeSolver) Solve(_ context.Context, challenge []byte) ([]byte, error) {
	f.got = append([]byte(nil), challenge...)
	return f.reply, f.err
}

func TestChallengePayloadStripsPrefix(t *testing.T) {
	// The game forwards exactly the bytes after the fixed FF 03 00 prefix to
	// its REST call; the pod expects that payload as the wire body.
	frame := []byte{0xFF, 0x03, 0x00, 0x0A, 0x21, 0x02, 0xAB}
	if got := challengePayload(frame); len(got) != 4 || got[0] != 0x0A || got[3] != 0xAB {
		t.Fatalf("challengePayload = % X, want 0A 21 02 AB", got)
	}
	for _, short := range [][]byte{nil, {0xFF}, {0xFF, 0x03}, {0xFF, 0x03, 0x00}} {
		if got := challengePayload(short); got != nil {
			t.Fatalf("challengePayload(% X) = % X, want nil", short, got)
		}
	}
}

func TestSolveFrame(t *testing.T) {
	// Success: the signed reply follows the ff 04 00 prefix. Failure or no
	// solver: the plain empty ack, exactly today's behaviour.
	challenge := []byte{0x0A, 0x21}
	reply := []byte{0xAA, 0xBB}
	ok := &fakeSolver{reply: reply}
	if got := solveFrame(context.Background(), ok, challenge); len(got) != 5 || got[0] != 0xFF || got[1] != 0x04 || got[2] != 0x00 || got[3] != 0xAA || got[4] != 0xBB {
		t.Fatalf("solveFrame = % X, want FF 04 00 AA BB", got)
	}
	if len(ok.got) != 2 || ok.got[0] != 0x0A {
		t.Fatalf("solver got % X, want the challenge payload", ok.got)
	}

	bad := &fakeSolver{err: errors.New("offline")}
	if got := solveFrame(context.Background(), bad, challenge); len(got) != 3 {
		t.Fatalf("failed solve = % X, want the empty ack", got)
	}
	if got := solveFrame(context.Background(), nil, challenge); len(got) != 3 {
		t.Fatalf("nil solver = % X, want the empty ack", got)
	}
}

func TestLatestSolveKeepsNewest(t *testing.T) {
	// While one solve is in flight, a burst of challenges must collapse to the
	// newest one: the pod repeats its challenge at connect, and a stale reply
	// must never be written after a newer one.
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var mu sync.Mutex
	var got []byte
	var runs int
	q := newLatestSolve(func(b []byte) {
		mu.Lock()
		runs++
		got = append(got, b[0])
		n := runs
		mu.Unlock()
		if n == 1 {
			close(started)
			<-release
		}
		if n == 2 {
			close(done)
		}
	})
	q.push([]byte{1})
	<-started
	q.push([]byte{2})
	q.push([]byte{3})
	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second solve never ran")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("solves = %v, want [1 3]", got)
	}
}

func TestLatestSolveCopiesTheChallenge(t *testing.T) {
	// The payload is a slice of the BLE notification buffer. The queue must own
	// a copy: the stack may reuse or invalidate its buffer before the solve
	// (or a queued solve) reads it.
	challenge := []byte{7, 8}
	got := make(chan byte, 1)
	q := newLatestSolve(func(b []byte) { got <- b[0] })
	q.push(challenge)
	challenge[0] = 9
	if b := <-got; b != 7 {
		t.Fatalf("solve saw %d, want the copy 7", b)
	}
}

func TestLatestSolveSurvivesAPanickingSolve(t *testing.T) {
	// recoverLog guards the worker: a bad solver must not kill the goroutine
	// and leave every later challenge unanswered.
	pushed := make(chan struct{})
	ran := make(chan struct{})
	q := newLatestSolve(func(b []byte) {
		if b[0] == 1 {
			close(pushed)
			panic("solver blew up")
		}
		close(ran)
	})
	q.push([]byte{1})
	<-pushed
	q.push([]byte{2})
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the solve worker did not survive the panic")
	}
}

func TestPeriodicAckEnabled(t *testing.T) {
	saved := SendAck
	defer func() { SendAck = saved }()
	SendAck = true
	if !periodicAckEnabled(nil) {
		t.Fatal("no solver: the historical empty ack ladder stays on")
	}
	if periodicAckEnabled(&fakeSolver{}) {
		t.Fatal("a solver replaces the empty ack ladder")
	}
	SendAck = false
	if periodicAckEnabled(nil) {
		t.Fatal("SendAck=false disables the ack ladder")
	}
}

func TestEndRequestConsumedOnce(t *testing.T) {
	EndSessionNow("AA:BB:CC:DD:EE:FF")
	if !takeEndRequest("AA:BB:CC:DD:EE:FF") {
		t.Fatal("the end request must be visible once")
	}
	if takeEndRequest("AA:BB:CC:DD:EE:FF") {
		t.Fatal("the end request must be consumed")
	}
}

func TestEndSessionsForSideArmsLiveSessions(t *testing.T) {
	// Seed the live-session index directly (no real BLE session).
	liveMu.Lock()
	liveAddrs = map[string]zwift.Pod{
		"AA:BB:CC:DD:EE:01": zwift.PodLeft,
		"AA:BB:CC:DD:EE:02": zwift.PodLeft,
		"AA:BB:CC:DD:EE:03": zwift.PodRight,
	}
	liveMu.Unlock()
	defer func() {
		liveMu.Lock()
		liveAddrs = map[string]zwift.Pod{}
		liveMu.Unlock()
	}()

	got := EndSessionsForSide(zwift.PodLeft)
	if len(got) != 2 {
		t.Fatalf("EndSessionsForSide(Left) armed %d requests, want 2: %v", len(got), got)
	}
	want := map[string]bool{"AA:BB:CC:DD:EE:01": true, "AA:BB:CC:DD:EE:02": true}
	for _, a := range got {
		if !want[a] {
			t.Fatalf("unexpected addr armed: %s", a)
		}
		if !takeEndRequest(a) {
			t.Fatalf("end request not armed for %s", a)
		}
	}
	// The right pod must not be armed for the Left side.
	if takeEndRequest("AA:BB:CC:DD:EE:03") {
		t.Fatal("right-pod addr must not be armed for Left")
	}
}

type solverFunc func(context.Context, []byte) ([]byte, error)

func (f solverFunc) Solve(ctx context.Context, b []byte) ([]byte, error) { return f(ctx, b) }

func TestSolveAndWriteSignedAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name   string
		solver ChallengeSolver
		want   []byte
	}{
		{"signed", &fakeSolver{reply: []byte{0xab}}, []byte{0xff, 4, 0, 0xab}},
		{"offline", &fakeSolver{err: errors.New("offline")}, []byte{0xff, 4, 0}},
		{"timeout", solverFunc(func(ctx context.Context, _ []byte) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }), []byte{0xff, 4, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			w := &sessionWriter{write: func(b []byte) (int, error) { got = append([]byte(nil), b...); return len(b), nil }}
			solveAndWrite(context.Background(), tc.solver, []byte{1}, w, time.Millisecond)
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("reply=%x want %x", got, tc.want)
			}
			if w.LastAck().IsZero() {
				t.Fatal("successful reply not anchored")
			}
		})
	}
}

func TestSolveAndWriteCancellationAndClosedWriter(t *testing.T) {
	for _, cancelSession := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelSession), func(t *testing.T) {
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var writes atomic.Int32
			w := &sessionWriter{write: func(b []byte) (int, error) { writes.Add(1); return len(b), nil }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			solver := solverFunc(func(context.Context, []byte) ([]byte, error) { close(entered); <-release; return []byte{0xab}, nil })
			go func() { defer close(done); solveAndWrite(ctx, solver, []byte{1}, w, time.Second) }()
			<-entered
			if cancelSession {
				cancel()
			} else {
				w.Close()
			}
			close(release)
			<-done
			if writes.Load() != 0 {
				t.Fatal("late solver reply reached transport")
			}
			if !w.LastAck().IsZero() {
				t.Fatal("late reply advanced anchor")
			}
		})
	}
}
