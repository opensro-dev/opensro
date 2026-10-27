/*
===========================================================================

shape_test.go - the draw and delay-queue behaviour of one direction

Draw-level tests read pass() results only (no sleeping), so they are
exact; queue tests use short real delays with generous margins, because
emission timing is the behaviour under test there.

===========================================================================
*/
package main

import (
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
TestPassDropsEverythingAtFullLoss
================
*/
func TestPassDropsEverythingAtFullLoss(t *testing.T) {
	shaper := newShaper(shapeConfig{LossPct: 100}, 7)
	for i := 0; i < 100; i++ {
		if admitted, _ := shaper.pass(); admitted {
			t.Fatal("loss 100 admitted a packet")
		}
	}
}

/*
================
TestPassAdmitsEverythingAtZeroLoss
================
*/
func TestPassAdmitsEverythingAtZeroLoss(t *testing.T) {
	shaper := newShaper(shapeConfig{Delay: time.Millisecond}, 7)
	for i := 0; i < 100; i++ {
		admitted, delay := shaper.pass()
		if !admitted {
			t.Fatal("loss 0 dropped a packet")
		}
		if delay != time.Millisecond {
			t.Fatalf("delay = %v, want the configured 1ms", delay)
		}
	}
}

/*
================
TestPassDelaysStayInsideJitterBounds

The delay is the configured one-way delay plus a uniform draw in
[-jitter, +jitter]: every draw must land inside the band, and over 500
draws both halves of the band must appear (the draw is not clamped to
one side).
================
*/
func TestPassDelaysStayInsideJitterBounds(t *testing.T) {
	shaper := newShaper(shapeConfig{Delay: 100 * time.Millisecond, Jitter: 40 * time.Millisecond}, 11)
	below, above := false, false
	for i := 0; i < 500; i++ {
		_, delay := shaper.pass()
		if delay < 60*time.Millisecond || delay > 140*time.Millisecond {
			t.Fatalf("delay %v outside [60ms, 140ms]", delay)
		}
		below = below || delay < 100*time.Millisecond
		above = above || delay > 100*time.Millisecond
	}
	if !below || !above {
		t.Fatalf("jitter drew on one side only (below %v, above %v)", below, above)
	}
}

/*
================
TestPassDelayFloorsAtZero

A jitter draw wider than the base delay must not produce a negative
delay (which would mean emitting into the past).
================
*/
func TestPassDelayFloorsAtZero(t *testing.T) {
	shaper := newShaper(shapeConfig{Delay: 5 * time.Millisecond, Jitter: 50 * time.Millisecond}, 13)
	for i := 0; i < 500; i++ {
		if _, delay := shaper.pass(); delay < 0 {
			t.Fatalf("delay %v is negative", delay)
		}
	}
}

/*
================
TestShaperSeedReproducesDraws

The same seed must reproduce the same admission/delay sequence, so a
measurement run can be repeated draw for draw.
================
*/
func TestShaperSeedReproducesDraws(t *testing.T) {
	cfg := shapeConfig{Delay: 30 * time.Millisecond, Jitter: 10 * time.Millisecond, LossPct: 25}
	first := newShaper(cfg, 42)
	second := newShaper(cfg, 42)
	for i := 0; i < 200; i++ {
		admittedA, delayA := first.pass()
		admittedB, delayB := second.pass()
		if admittedA != admittedB || delayA != delayB {
			t.Fatalf("draw %d differs: (%v, %v) vs (%v, %v)", i, admittedA, delayA, admittedB, delayB)
		}
	}
}

/*
================
TestLegShapersDoNotShareDraws

Each leg derives its own streams from the run seed: drawing on one leg
must not shift another leg's sequence, and the same leg derived again
repeats exactly. The legs used to share one pair of shapers, so an
A/B run with both legs active reproduced neither (#1821).
================
*/
func TestLegShapersDoNotShareDraws(t *testing.T) {
	values := flagValues{
		upDelay: 30 * time.Millisecond, upJitter: 10 * time.Millisecond, upLoss: 25,
		downDelay: 30 * time.Millisecond, downJitter: 10 * time.Millisecond, downLoss: 25,
		seed: 99,
	}
	tcpA, _, _ := legShapers(values, drawSeed(values), tcpLegSalt)
	udpA, _, _ := legShapers(values, drawSeed(values), udpLegSalt)
	tcpB, _, _ := legShapers(values, drawSeed(values), tcpLegSalt)
	for i := 0; i < 200; i++ {
		admittedA, delayA := tcpA.pass()
		udpA.pass() // the other leg drawing must not perturb tcpA's stream
		admittedB, delayB := tcpB.pass()
		if admittedA != admittedB || delayA != delayB {
			t.Fatalf("draw %d: the tcp sequence shifted with the udp leg's draws: (%v, %v) vs (%v, %v)",
				i, admittedA, delayA, admittedB, delayB)
		}
	}
}

/*
================
TestLegShapersDeriveDownAndSessions

The seed derivation below a leg's up stream (#1821): the down
direction draws from the run seed through the direction salt, so one
leg's up and down streams differ while two identically seeded legs
reproduce their down streams draw for draw. Each UDP session stirs
the leg's down seed with its creation order, so same-order sessions
on two legs reproduce and neighbouring sessions differ.
================
*/
func TestLegShapersDeriveDownAndSessions(t *testing.T) {
	values := flagValues{
		upDelay: 30 * time.Millisecond, upJitter: 10 * time.Millisecond, upLoss: 25,
		downDelay: 30 * time.Millisecond, downJitter: 10 * time.Millisecond, downLoss: 25,
		seed: 99,
	}
	seed := drawSeed(values)
	up, downCfg, downSeed := legShapers(values, seed, udpLegSalt)
	_, downCfgB, downSeedB := legShapers(values, seed, udpLegSalt)
	if downSeed != downSeedB || downCfg != downCfgB {
		t.Fatalf("down derivation differs across identical legs: seed %d vs %d", downSeed, downSeedB)
	}

	// identical reports whether two shapers draw the same 200
	// admissions and delays in lockstep.
	identical := func(a, b *shaper) bool {
		for i := 0; i < 200; i++ {
			admittedA, delayA := a.pass()
			admittedB, delayB := b.pass()
			if admittedA != admittedB || delayA != delayB {
				return false
			}
		}
		return true
	}

	// The leg's single down stream (the TCP leg's shape) reproduces
	// across legs and never equals the leg's up stream.
	if !identical(newShaper(downCfg, downSeed), newShaper(downCfgB, downSeedB)) {
		t.Fatal("identically seeded legs drew different down streams")
	}
	if identical(up, newShaper(downCfg, downSeed)) {
		t.Fatal("the down stream equals the up stream: the direction salt is missing")
	}

	// Sessions minted in the same order on two identically seeded legs
	// reproduce draw for draw; neighbouring sessions on one leg differ.
	legA := &udpLeg{downCfg: downCfg, downSeed: downSeed}
	legB := &udpLeg{downCfg: downCfgB, downSeed: downSeedB}
	firstA, secondA := legA.sessionDownShaper(), legA.sessionDownShaper()
	firstB, secondB := legB.sessionDownShaper(), legB.sessionDownShaper()
	if !identical(firstA, firstB) || !identical(secondA, secondB) {
		t.Fatal("same-order sessions on identically seeded legs drew differently")
	}
	if identical(firstA, secondA) {
		t.Fatal("sessions 1 and 2 drew identically: the creation-order stir is missing")
	}
}

/*
================
emissionLog
================
*/
type emissionLog struct {
	mu    sync.Mutex
	names []string
	at    []time.Time
}

/*
================
emit
================
*/
func (log *emissionLog) emit(name string) func([]byte) error {
	return func([]byte) error {
		log.mu.Lock()
		log.names = append(log.names, name)
		log.at = append(log.at, time.Now())
		log.mu.Unlock()
		return nil
	}
}

/*
================
TestDelayQueueEmitsEarliestFirst

A datagram submitted later but due earlier leaves first, as jitter on a
real link reorders; ties keep submission order.
================
*/
func TestDelayQueueEmitsEarliestFirst(t *testing.T) {
	queue := newDelayQueue()
	go queue.run()
	defer queue.close()
	log := &emissionLog{}
	start := time.Now()
	queue.submit(start.Add(90*time.Millisecond), []byte("late"), log.emit("late"))
	queue.submit(start.Add(10*time.Millisecond), []byte("early"), log.emit("early"))
	queue.submit(start.Add(10*time.Millisecond), []byte("tied"), log.emit("tied"))
	wait.Eventually(t, 3*time.Second, "all three emissions", func() bool {
		log.mu.Lock()
		defer log.mu.Unlock()
		return len(log.names) == 3
	})
	log.mu.Lock()
	defer log.mu.Unlock()
	want := []string{"early", "tied", "late"}
	for i, name := range want {
		if log.names[i] != name {
			t.Fatalf("emission order %v, want %v", log.names, want)
		}
	}
	if elapsed := log.at[0].Sub(start); elapsed < 5*time.Millisecond {
		t.Fatalf("first emission after %v, want the ~10ms schedule respected", elapsed)
	}
}

/*
================
TestDelayQueueEmitsNothingBeforeItsTime

Nothing leaves early: with a 60ms schedule, the queue is still empty at
30ms and has emitted by 300ms.
================
*/
func TestDelayQueueEmitsNothingBeforeItsTime(t *testing.T) {
	queue := newDelayQueue()
	go queue.run()
	defer queue.close()
	log := &emissionLog{}
	queue.submit(time.Now().Add(60*time.Millisecond), []byte("x"), log.emit("x"))
	wait.Consistently(t, 30*time.Millisecond, "no emission before its scheduled time", func() bool {
		log.mu.Lock()
		defer log.mu.Unlock()
		return len(log.names) == 0
	})
	wait.Eventually(t, 2*time.Second, "the scheduled emission", func() bool {
		log.mu.Lock()
		defer log.mu.Unlock()
		return len(log.names) == 1
	})
}

/*
================
TestDelayQueueCloseReportsDiscarded

Items still queued when the queue closes are reported as discarded
(the leg counts them as failed), and a closed queue refuses new
submissions instead of swallowing them into a dead heap.
================
*/
func TestDelayQueueCloseReportsDiscarded(t *testing.T) {
	queue := newDelayQueue()
	go queue.run()
	log := &emissionLog{}
	if !queue.submit(time.Now().Add(time.Hour), []byte("never"), log.emit("never")) {
		t.Fatal("submit to an open queue was refused")
	}
	if discarded := queue.close(); discarded != 1 {
		t.Fatalf("close reported %d discarded, want 1", discarded)
	}
	if queue.submit(time.Now(), []byte("late"), log.emit("late")) {
		t.Fatal("submit after close was accepted")
	}
	wait.Consistently(t, 20*time.Millisecond, "no emission from a closed queue", func() bool {
		log.mu.Lock()
		defer log.mu.Unlock()
		return len(log.names) == 0
	})
}
