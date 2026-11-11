// Package processguard serializes local launches of one native service.
// It complements, but never replaces, distributed leases and authority locks.
package processguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const retryInterval = 100 * time.Millisecond

// Guard holds a process-scoped operating-system file lock.
type Guard struct {
	file *os.File
}

// Acquire waits until no sibling process holds path. Antivirus sandboxes can
// execute a captured binary with its real environment before allowing the
// scheduler-tracked process to continue; serializing before network or store
// ownership prevents that transient copy from fencing the real allocation.
func Acquire(ctx context.Context, path string) (*Guard, error) {
	if ctx == nil {
		return nil, fmt.Errorf("process guard requires a context")
	}
	if path == "" {
		return nil, fmt.Errorf("process guard path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create process guard directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open process guard: %w", err)
	}
	for {
		err = tryLock(file)
		if err == nil {
			return &Guard{file: file}, nil
		}
		if !lockContended(err) {
			_ = file.Close()
			return nil, fmt.Errorf("acquire process guard: %w", err)
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, fmt.Errorf("acquire process guard: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

/*
================
TryAcquire

One attempt at the guard, for a caller that has somewhere to be while a
sibling holds it (the standby handover, opensro-ops#281): held reports
whether this process won the lock. A contended lock is not an error.
================
*/
func TryAcquire(path string) (guard *Guard, held bool, err error) {
	if path == "" {
		return nil, false, fmt.Errorf("process guard path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, fmt.Errorf("create process guard directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("open process guard: %w", err)
	}
	if err := tryLock(file); err != nil {
		_ = file.Close()
		if lockContended(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("acquire process guard: %w", err)
	}
	return &Guard{file: file}, true, nil
}

// Close releases the guard. The operating system also releases it on any
// process death, so no stale sentinel can block a later Nomad allocation.
func (guard *Guard) Close() error {
	if guard == nil || guard.file == nil {
		return nil
	}
	err := errors.Join(unlock(guard.file), guard.file.Close())
	guard.file = nil
	return err
}
