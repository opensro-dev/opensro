/*
===========================================================================

lockfile.go - the store's two-layer process lock

===========================================================================
*/

package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/platform/privatepath"
)

// LockFileName sits in the authority directory while a process owns the
// store. The guard is TWO layers deep:
//
//   - an OS advisory lock on the file (flock on Unix, LockFileEx on
//     Windows - lockfile_other.go / lockfile_windows.go), held for the
//     owner's whole lifetime and released by the OS on ANY death, clean
//     or killed. This is the authoritative gate: it is taken BEFORE the
//     pid payload is read or written, so two racing openers serialize on
//     it instead of both passing a read-then-write window.
//   - a JSON pid payload inside the file, inspectable like every other
//     state artifact. It is operator diagnostics AND the guard against
//     writers that hold no OS lock (a hand-written lock, an older
//     binary): a payload naming a LIVE foreign pid refuses even when the
//     OS lock was grantable.
//
// The kill model still holds: kills release the OS lock automatically and
// leave only the stale payload, which the next boot replaces with a
// warning. The migration tool takes the same lock and refuses to --commit
// while the server runs (the dual-writer guard).
const LockFileName = "authority.lock"

// lockFilePayload is what the lock holds - inspectable like every other
// state artifact.
type lockFilePayload struct {
	PID       int   `json:"pid"`
	StartedAt int64 `json:"startedAtMs"`
}

// creationProbe resolves a process's creation time; a test seam over the
// platform implementation.
var creationProbe = processCreationUnixMilli

// recycledPIDMarginMs absorbs FILETIME-vs-wallclock rounding when
// comparing a candidate's creation time against the lock write instant.
// The real owner is born SECONDS before it writes the lock; a recycled
// PID is born strictly after the old owner died, i.e. after the write.
const recycledPIDMarginMs = 500

// claims is the process-wide registry of open authority directories,
// keyed by the canonical lock-file path, each holding the OS-locked
// lock-file handle. It exists because the OS lock alone cannot arbitrate
// IN-process openers (a second handle of the same process fails the
// exclusive lock exactly like a foreign process would), and because two
// live Store instances writing one state.db is a real corruption path
// this package refuses outright.
var (
	claimsMu sync.Mutex
	claims   = map[string]*os.File{}
)

/*
==================
claimKey

claimKey canonicalizes a lock path into the registry key. Windows paths
compare case-insensitively; aliased spellings the key cannot fold
(symlinks, 8.3 names) still refuse safely - the second opener fails the
OS lock on the same underlying file.
==================
*/
func claimKey(path string) string {
	key, err := filepath.Abs(path)
	if err != nil {
		key = filepath.Clean(path)
	}
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

/*
==================
claimAuthority

claimAuthority is Open's single-writer gate. First claim of a directory
acquires the OS lock and writes the pid payload (acquireLock). A SECOND
in-process claim of the same directory is a dual-writer hazard and
REFUSES unconditionally - one process is one authority, full stop.
Callers that model a restart (the door-persistence suites) Close the
first instance before opening the second, exactly like a real reboot.

The returned release function must be called exactly once per successful
claim (Store.Close does); releasing drops the OS lock.
==================
*/
func claimAuthority(dir string, now time.Time) (func(), string, error) {
	path := filepath.Join(dir, LockFileName)
	key := claimKey(path)
	claimsMu.Lock()
	defer claimsMu.Unlock()
	if _, open := claims[key]; open {
		return nil, "", fmt.Errorf("authority store %s is already open in this process: a second live Store instance would dual-write %s - Close the first instance first", dir, DBFileName)
	}
	file, warning, err := acquireLock(path, now)
	if err != nil {
		return nil, "", err
	}
	claims[key] = file
	return releaseClaimFunc(key), warning, nil
}

// releaseClaimFunc builds the claim's release: release the lock and
// forget the directory.
func releaseClaimFunc(key string) func() {
	return func() {
		claimsMu.Lock()
		defer claimsMu.Unlock()
		file := claims[key]
		if file == nil {
			return
		}
		releaseLock(file)
		delete(claims, key)
	}
}

/*
==================
releaseLock

The one clean release of an acquireLock handle. The pid payload is
cleared while the OS lock is still held, then the lock drops: a clean
release leaves no owner behind. Without the clear, the closed owner's
pid outlives its lock, and while that process is still exiting the
next opener reads it as a live writer and refuses - the canary standby
takeover's crash on 2026-11-20 (r203), 83 ms after the old GameWorld's
"shutdown: complete". A kill skips this and leaves the payload, which
the dead-pid rule in acquireLock reclaims.
==================
*/
func releaseLock(file *os.File) {
	_ = rewriteLockedPayload(file, nil)
	_ = unlockFileExclusive(file)
	_ = file.Close()
}

/*
==================
acquireLock

acquireLock takes the OS advisory lock on path, refuses when another
LIVE process holds the directory, and (over)writes the payload with our
pid otherwise. Returns the locked handle (the caller holds it for the
store's lifetime) and a warning string for the stale-lock case (empty =
clean acquire).

The OS lock comes FIRST: the old read-check-write sequence had a TOCTOU
window where two processes both read a stale payload, both judged it
dead, and both wrote their own. Serializing on the OS lock closes it,
and the OS releasing the lock at process death is what keeps the
stale-lock-from-a-dead-owner reclaim working with no operator hand.

A pid can be recycled within milliseconds of a process exit, which would read as a
"live owner" and crash-loop the reboot until an operator deletes the
lock. The lock carries its write instant, so a candidate whose
CREATION time is after that instant is provably not the owner - the
lock is stale no matter how alive the pid is.
==================
*/
func acquireLock(path string, now time.Time) (*os.File, string, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("opening authority lock: %w", err)
	}
	if err := privatepath.ProtectFile(path); err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("securing authority lock permissions: %w", err)
	}
	if err := lockFileExclusive(file); err != nil {
		file.Close()
		return nil, "", fmt.Errorf("authority store is locked by another live writer (%s): the OS advisory lock is held - stop that process first (%v)", path, err)
	}
	refuse := func(cause error) (*os.File, string, error) {
		_ = unlockFileExclusive(file)
		_ = file.Close()
		return nil, "", cause
	}

	warning := ""
	payload, readErr := os.ReadFile(path)
	if readErr == nil {
		previous := lockFilePayload{}
		if json.Unmarshal(payload, &previous) == nil && previous.PID > 0 && previous.PID != os.Getpid() {
			if pidAlive(previous.PID) {
				if creation, ok := creationProbe(previous.PID); ok && creation > previous.StartedAt+recycledPIDMarginMs {
					warning = fmt.Sprintf("authority lock pid %d was RECYCLED (process born %dms after the lock was written); treating the lock as stale", previous.PID, creation-previous.StartedAt)
				} else {
					// A live owner that holds no OS lock (hand-written
					// lock, or a writer predating the OS-lock layer):
					// the payload is the only guard there is - refuse.
					return refuse(fmt.Errorf("authority store is locked by live pid %d (%s); a second writer would corrupt the single-authority guarantee - stop that process first", previous.PID, path))
				}
			} else {
				warning = fmt.Sprintf("stale authority lock from dead pid %d replaced", previous.PID)
			}
		} else if len(payload) > 0 && previous.PID == 0 {
			warning = fmt.Sprintf("unreadable authority lock at %s replaced", path)
		}
	} else if !os.IsNotExist(readErr) {
		return refuse(fmt.Errorf("reading authority lock: %w", readErr))
	}

	fresh, err := json.Marshal(lockFilePayload{PID: os.Getpid(), StartedAt: now.UnixMilli()})
	if err != nil {
		return refuse(err)
	}
	// The payload rewrites through the LOCKED handle (truncate + write +
	// fsync), never via rename: renaming over the locked file would drop
	// the very lock this function just took. A kill mid-rewrite leaves a
	// torn payload, which the next boot reports as "unreadable authority
	// lock replaced" - the OS lock, not the payload, is what that boot's
	// safety rides on.
	if err := rewriteLockedPayload(file, fresh); err != nil {
		return refuse(fmt.Errorf("writing authority lock: %w", err))
	}
	return file, warning, nil
}

// rewriteLockedPayload replaces the locked lock file's content in place.
func rewriteLockedPayload(file *os.File, payload []byte) error {
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.WriteAt(payload, 0); err != nil {
		return err
	}
	return syncFile(file)
}

/*
==================
pidAlive

pidAlive reports actual process liveness through the platform probe. On
Windows an exited process object can remain open briefly after its command
has returned, so merely opening the PID is not a liveness verdict:
processAlive must also prove the exit code is STILL_ACTIVE.
==================
*/
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return processAlive(pid)
}
