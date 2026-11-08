/*
===========================================================================

deps_read_door_test.go - the value read door holds and releases its lock

AcquireRead takes the same store read lock the callback door runs
between, held as a value the caller releases in line: the per-tick
pet judges (#2030) read a handful of scalars through it each tick
without boxing a closure per call. These tests pin the guard's
contract - the lock is taken on acquire and released exactly once,
a second release is harmless, and a Deps without the lock pair reads
unguarded, exactly as the callback door does when ReadCharacter is
unset.

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestReadGuardHoldsAndReleasesOnce
================
*/
func TestReadGuardHoldsAndReleasesOnce(t *testing.T) {
	locks, unlocks, depth := 0, 0, 0
	deps := &Deps{
		ReadLock:   func(string) { locks++; depth++ },
		ReadUnlock: func(string) { unlocks++; depth-- },
	}
	guard := deps.AcquireRead("division")
	if locks != 1 || depth != 1 {
		t.Fatalf("after acquire: locks=%d depth=%d, want 1/1", locks, depth)
	}
	guard.Release()
	guard.Release()
	if unlocks != 1 || depth != 0 {
		t.Fatalf("after two releases: unlocks=%d depth=%d, want 1/0 (release is idempotent)", unlocks, depth)
	}
}

/*
================
TestReadGuardWithoutLockPairIsUnguarded
================
*/
func TestReadGuardWithoutLockPairIsUnguarded(t *testing.T) {
	deps := &Deps{}
	guard := deps.AcquireRead("division")
	guard.Release()
}
