package store_test

/*
===========================================================================

open_context_test.go - the cancelled store open (#3186)

An interrupt during the open must end it promptly, release the
authority claim, and never touch the database's bytes: no quarantine,
no bak recovery. The OpenProbe seam holds one step open so the test
cancels into it deterministically.
===========================================================================
*/

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/data/store"
)

/*
================
TestOpenCancelledReturnsPromptly

The probe holds the open until its context ends; the cancel lands
mid-open and Open must return the context's error. A second open of
the same directory then succeeds - which proves the claim was
released - and the database file is still exactly where a good open
left it.
================
*/
func TestOpenCancelledReturnsPromptly(t *testing.T) {
	dir := t.TempDir()

	// A real authority first, so the open the test cancels has a main
	// database in front of it (the production posture, not the
	// first-boot rung).
	first, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first.Close()

	ctx, cancel := context.WithCancel(context.Background())
	probe := func(probeCtx context.Context, _ string) error {
		<-probeCtx.Done()
		return probeCtx.Err()
	}
	started := time.Now()
	opened := make(chan error, 1)
	go func() {
		_, openErr := store.Open(dir, store.Options{Context: ctx, OpenProbe: probe})
		opened <- openErr
	}()
	cancel()
	select {
	case openErr := <-opened:
		if !errors.Is(openErr, context.Canceled) {
			t.Fatalf("cancelled open returned %v, want the context error", openErr)
		}
		if took := time.Since(started); took > time.Second {
			t.Fatalf("cancelled open took %s, want under a second", took)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled open did not return")
	}

	second, err := store.Open(dir, store.Options{})
	if err != nil {
		t.Fatalf("second open after the cancelled one: %v (the claim must be released)", err)
	}
	second.Close()

	// Nothing was quarantined: the directory still holds exactly the
	// main database (a *.corrupt sibling would mean the cancellation
	// fell into the recovery rungs).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".corrupt-") {
			t.Fatalf("cancelled open quarantined %s; a cancellation is not corruption", entry.Name())
		}
	}
}
