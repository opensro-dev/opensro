/*
===========================================================================

health_test.go - the published write condition answers without the lock

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"
)

// healthAnswerBound bounds how long Health may take while the store lock
// is held; it answers from an atomic, so any wait at all is the defect.
const healthAnswerBound = 5 * time.Second

/*
================
TestHealthAnswersWhileTheStoreLockIsHeld

The readiness probe reads Health (#570). With the store's write lock held,
as by a writer the probe would otherwise queue behind, Health still
answers, with the failure the last commit published.
================
*/
func TestHealthAnswersWhileTheStoreLockIsHeld(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	s.commitFail = errors.New("disk failure")
	s.Mutate("health-probe", func() {})
	s.commitFail = nil
	s.mu.Lock()
	answered := make(chan Health, 1)
	go func() { answered <- s.Health() }()
	select {
	case health := <-answered:
		if health.FailedWrites != 1 || health.LastError == "" {
			t.Errorf("published health = %+v, want one failed write", health)
		}
	case <-time.After(healthAnswerBound):
		t.Error("Health waited for the store lock")
	}
	s.mu.Unlock()
	s.Mutate("health-recovery", func() {})
	if health := s.Health(); health.FailedWrites != 0 || health.LastCommitAt.IsZero() {
		t.Fatalf("recovered health = %+v, want no failed writes", health)
	}
}
