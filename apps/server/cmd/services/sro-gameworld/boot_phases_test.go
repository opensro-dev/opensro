package main

/*
===========================================================================

boot_phases_test.go - admission carries the boot to ready (#3186)

There is no full-process boot harness in this package (see
wtpublic_test.go); the seams carry the proof instead. This one
constructs the bare application the way newGameWorldApplication does -
transport, gate, shard - and walks admit() with no population to fill:
the boot must land on ready with the filling phase closed behind it.
===========================================================================
*/

import (
	"context"
	"testing"

	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/transport"
)

/*
================
TestAdmitClosesBootAtReady

admit() on an empty population settles at once: the phase becomes
ready, and the recorded phases show the filling step's duration.
================
*/
func TestAdmitClosesBootAtReady(t *testing.T) {
	ts, err := transport.NewServer(transport.Config{
		WTAddr:  "127.0.0.1:0",
		WSAddr:  "127.0.0.1:0",
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	application := &gameWorldApplication{
		transport: ts,
		readiness: readiness.NewGate(),
		shardID:   "boot-phase-test",
	}
	if err := application.admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	status := ts.BootStatus()
	if status.Phase != transport.BootPhaseReady {
		t.Fatalf("phase after admit = %q, want %q", status.Phase, transport.BootPhaseReady)
	}
	var filling *transport.BootPhaseTiming
	for i := range status.Phases {
		if status.Phases[i].Name == transport.BootPhaseFilling {
			filling = &status.Phases[i]
		}
	}
	if filling == nil {
		t.Fatalf("closed phases = %+v, want the filling phase recorded", status.Phases)
	}
}
