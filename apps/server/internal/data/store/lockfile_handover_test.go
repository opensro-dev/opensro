/*
===========================================================================

lockfile_handover_test.go - the store lock across a process handover

The canary standby handover (opensro-ops#281) opens the authority store
the moment the old GameWorld releases its guard, while the old process
is still exiting. These tests run a real second process: the helper
opens and closes the store, then stays alive, exactly like the old
GameWorld between "shutdown: complete" and its exit.

===========================================================================
*/

package store

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	envLockHandoverHelper = "SRO_LOCK_HANDOVER_HELPER"
	envLockHandoverDir    = "SRO_LOCK_HANDOVER_DIR"
	envLockHandoverClose  = "SRO_LOCK_HANDOVER_CLOSE"
	lockHandoverReady     = "store-ready"
)

/*
================
startLockHandoverHelper

Run this test binary as a second process that opens the store in dir,
closes it when closeStore is set, prints lockHandoverReady and then
lives until the returned release function closes its stdin.
================
*/
func startLockHandoverHelper(t *testing.T, dir string, closeStore bool) func() {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestLockHandoverHelperProcess$")
	closeValue := "0"
	if closeStore {
		closeValue = "1"
	}
	command.Env = append(os.Environ(),
		envLockHandoverHelper+"=1",
		envLockHandoverDir+"="+dir,
		envLockHandoverClose+"="+closeValue,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("helper stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("helper stdout: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	release := func() {
		_ = stdin.Close()
		_ = command.Wait()
	}
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if strings.TrimSpace(lines.Text()) == lockHandoverReady {
			// Drain the rest so the helper never blocks on a full pipe.
			go func() {
				for lines.Scan() {
				}
			}()
			return release
		}
	}
	release()
	t.Fatalf("helper exited before it opened the store")
	return nil
}

/*
================
TestLockHandoverHelperProcess

The helper body; a no-op unless startLockHandoverHelper launched it.
================
*/
func TestLockHandoverHelperProcess(t *testing.T) {
	if os.Getenv(envLockHandoverHelper) != "1" {
		return
	}
	s, err := Open(os.Getenv(envLockHandoverDir), Options{})
	if err != nil {
		os.Stderr.WriteString("helper open: " + err.Error() + "\n")
		os.Exit(2)
	}
	if os.Getenv(envLockHandoverClose) == "1" {
		s.Close()
	}
	os.Stdout.WriteString(lockHandoverReady + "\n")
	// Alive until the parent releases us: the exiting old GameWorld.
	buffer := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(buffer); err != nil {
			break
		}
	}
	os.Exit(0)
}

/*
================
TestLockOpensAfterLiveOwnerClosedCleanly

2026-11-20 r203: the standby took over 83 ms after the old GameWorld's
"shutdown: complete" and refused with "locked by live pid": the old
process had dropped the OS lock but left its pid in the payload, and it
was still alive. A clean Close is a release, so the next opener must
get the store while the closed owner lives on.
================
*/
func TestLockOpensAfterLiveOwnerClosedCleanly(t *testing.T) {
	dir := t.TempDir()
	release := startLockHandoverHelper(t, dir, true)
	defer release()

	s, err := Open(dir, Options{})
	if err != nil {
		t.Fatalf("a store its live owner closed cleanly must open: %v", err)
	}
	s.Close()
}

/*
================
TestLockRefusesWhileLiveOwnerHoldsIt

The other side of the handover: an owner that has NOT closed still
refuses a second process, by the OS lock.
================
*/
func TestLockRefusesWhileLiveOwnerHoldsIt(t *testing.T) {
	dir := t.TempDir()
	release := startLockHandoverHelper(t, dir, false)
	defer release()

	if _, err := Open(dir, Options{}); err == nil {
		t.Fatal("a store held by a live process must refuse a second writer")
	}
	if _, err := os.Stat(filepath.Join(dir, LockFileName)); err != nil {
		t.Fatalf("the held lock file must stay: %v", err)
	}
}
