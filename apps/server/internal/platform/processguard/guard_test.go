package processguard

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAcquireSerializesAndRecoversAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.lock")
	first, err := Acquire(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan *Guard, 1)
	errors := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		second, acquireErr := Acquire(ctx, path)
		if acquireErr != nil {
			errors <- acquireErr
			return
		}
		acquired <- second
	}()

	select {
	case second := <-acquired:
		_ = second.Close()
		t.Fatal("second guard acquired while first was live")
	case err := <-errors:
		t.Fatalf("second guard failed while waiting: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case second := <-acquired:
		if err := second.Close(); err != nil {
			t.Fatal(err)
		}
	case err := <-errors:
		t.Fatalf("second guard failed after release: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("second guard did not acquire after release")
	}
}

/*
================
TestTryAcquireReportsHeldAndFree
================
*/
func TestTryAcquireReportsHeldAndFree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.lock")
	guard, held, err := TryAcquire(path)
	if err != nil || !held {
		t.Fatalf("a free guard = held %v, err %v, want the lock", held, err)
	}
	// A second fd in this same process still loses: the lock is the
	// file's, not the process's.
	if _, held, err := TryAcquire(path); err != nil || held {
		t.Fatalf("a live guard = held %v, err %v, want a clean miss", held, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	guard, held, err = TryAcquire(path)
	if err != nil || !held {
		t.Fatalf("a released guard = held %v, err %v, want the lock", held, err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := TryAcquire(""); err == nil {
		t.Fatal("an empty path must refuse, not lock something")
	}
}
