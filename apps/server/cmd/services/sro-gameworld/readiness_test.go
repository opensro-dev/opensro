/*
===========================================================================

readiness_test.go - the GameWorld readiness probe's decision order

===========================================================================
*/
package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/platform/readiness"
)

/*
================
fakeProgress
================
*/
type fakeProgress struct{ err error }

func (p *fakeProgress) Progress(time.Time) error { return p.err }

/*
================
TestReadinessReportsAStalledSimulationAndRecovers

The probe refuses while the process gate is closed, reports a stalled
simulation as not ready without consulting anything that can wait, and is
ready again once ticks finish (#570).
================
*/
func TestReadinessReportsAStalledSimulationAndRecovers(t *testing.T) {
	authority, err := store.Open(t.TempDir(), store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	gate, progress := readiness.NewGate(), &fakeProgress{}
	check := readyCheck(authority, gate, progress)
	if err := check(); err == nil || !strings.Contains(err.Error(), "not accepting") {
		t.Fatalf("closed gate answered %v", err)
	}
	gate.Open()
	if err := check(); err != nil {
		t.Fatalf("a ticking world answered %v", err)
	}
	progress.err = errors.New("simulation tick running for 6s")
	if err := check(); err == nil || !strings.Contains(err.Error(), "tick running") {
		t.Fatalf("a stalled tick answered %v", err)
	}
	progress.err = nil
	if err := check(); err != nil {
		t.Fatalf("the recovered world answered %v", err)
	}
}
