/*
===========================================================================

tick_watchdog_test.go - a stalled tick is reported once, by phase and hook

===========================================================================
*/

package simulation

import (
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// emptySessionSource and discardPusher give the ticker no sessions and no
// delivery, so only the hooks run.
type emptySessionSource struct{}

func (emptySessionSource) SnapshotSessions() []SessionSnapshot { return nil }

type discardPusher struct{}

func (discardPusher) PushToSession(string, []Frame)          {}
func (discardPusher) PushToDivision(string, []Frame, string) {}

/*
================
stalledTickHook

A named hook that blocks until released, standing in for a tick hook
waiting on a lock or a disk.
================
*/
func stalledTickHook(release <-chan struct{}) TickHook {
	return func(int64) []DivisionFrames {
		<-release
		return nil
	}
}

/*
================
TestTickWatchdogReportsAStalledHookOnce
================
*/
func TestTickWatchdogReportsAStalledHookOnce(t *testing.T) {
	release := make(chan struct{})
	ticker := &Ticker{Source: emptySessionSource{}, Push: discardPusher{}, Hooks: []TickHook{stalledTickHook(release)}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker.RunTick(0)
	}()

	wait.Eventually(t, 5*time.Second, "the stalled hook to start", func() bool {
		return ticker.watch.hook.Load() != 0
	})
	report := ticker.watch.check(time.Now().Add(2*time.Second), time.Second)
	if !strings.Contains(report, "in hooks (hook ") || !strings.Contains(report, "stalledTickHook") {
		t.Fatalf("report does not name the phase and hook: %.200s", report)
	}
	if !strings.Contains(report, "goroutine ") {
		t.Fatalf("report carries no goroutine stacks: %.200s", report)
	}
	if again := ticker.watch.check(time.Now().Add(3*time.Second), time.Second); again != "" {
		t.Fatal("the same stall was reported twice")
	}

	close(release)
	<-done
	if after := ticker.watch.check(time.Now().Add(time.Hour), time.Second); after != "" {
		t.Fatal("a finished tick was reported")
	}
}

/*
================
TestTickWatchdogIgnoresATickWithinTheThreshold
================
*/
func TestTickWatchdogIgnoresATickWithinTheThreshold(t *testing.T) {
	var watch tickWatch
	now := time.Now()
	watch.begin(now)
	if report := watch.check(now.Add(500*time.Millisecond), time.Second); report != "" {
		t.Fatalf("a 500 ms tick was reported: %.120s", report)
	}
}
