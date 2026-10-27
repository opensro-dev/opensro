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
	deadline := time.Now().Add(3 * time.Second)
	for {
		log.mu.Lock()
		done := len(log.names) == 3
		log.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("emitted %v, want all three", log.names)
		}
		time.Sleep(2 * time.Millisecond)
	}
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
	time.Sleep(30 * time.Millisecond)
	log.mu.Lock()
	if len(log.names) != 0 {
		t.Fatalf("emitted %v before its scheduled time", log.names)
	}
	log.mu.Unlock()
	deadline := time.Now().Add(2 * time.Second)
	for {
		log.mu.Lock()
		done := len(log.names) == 1
		log.mu.Unlock()
		if done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the scheduled emission never happened")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
