/*
===========================================================================

clock_test.go - a hand-driven clock for the watchdog tests

The park and entry-bound watchdogs used to read the wall clock, so
their tests proved scheduler speed: under a loaded machine the ack a
test had already sent could sit unprocessed past the bound and the
watchdog fired on lag, not on the condition (#2445). A hub on this
clock stamps and ticks only when the test advances it, so a bound
trips exactly when the test says the time has passed - and never
while the test is still making progress.

===========================================================================
*/
package transport

import (
	"sync"
	"testing"
	"time"
)

/*
================
manualClock

Time stands still until Advance. Every live ticker receives the new
time; delivery blocks until the keepalive loop takes it, so when an
Advance returns, each loop has begun that tick's checks, and one
further Advance proves the previous tick's checks have run.
================
*/
type manualClock struct {
	mu      sync.Mutex
	now     time.Time
	tickers map[*manualTicker]struct{}
}

// manualTicker is a ticker fed by its manualClock's Advance.
type manualTicker struct {
	clock *manualClock
	ch    chan time.Time
}

/*
================
newManualClock
================
*/
func newManualClock() *manualClock {
	return &manualClock{
		now:     time.Unix(1_700_000_000, 0),
		tickers: make(map[*manualTicker]struct{}),
	}
}

/*
================
manualClock.Now
================
*/
func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

/*
================
manualClock.NewTicker

The interval is the loop's business, not the clock's: a manual tick
arrives when the test advances, whatever interval was asked for.
================
*/
func (c *manualClock) NewTicker(time.Duration) ticker {
	t := &manualTicker{clock: c, ch: make(chan time.Time)}
	c.mu.Lock()
	c.tickers[t] = struct{}{}
	c.mu.Unlock()
	return t
}

/*
================
manualClock.Advance

Moves the clock forward and hands the new time to every live ticker.
A ticker whose loop has stopped is no longer registered (Stop), so
this never blocks on a dead loop.
================
*/
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	live := make([]*manualTicker, 0, len(c.tickers))
	for t := range c.tickers {
		live = append(live, t)
	}
	c.mu.Unlock()
	for _, t := range live {
		t.ch <- now
	}
}

/*
================
manualClock.install

Puts a hub on this clock. Install before any session is created:
the keepalive loop takes its ticker when its session starts.
================
*/
func (c *manualClock) install(hub *Hub) {
	hub.now = c.Now
	hub.newTicker = c.NewTicker
}

/*
================
manualClock.tickerCount

How many keepalive loops have taken their ticker. A test waits for
its session's loop to register before advancing: an advance with no
ticker registered would hand its tick to no one, and the watchdog
would never see that time pass.
================
*/
func (c *manualClock) tickerCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.tickers)
}

/*
================
manualTicker.C
================
*/
func (t *manualTicker) C() <-chan time.Time { return t.ch }

/*
================
manualTicker.Stop
================
*/
func (t *manualTicker) Stop() {
	t.clock.mu.Lock()
	delete(t.clock.tickers, t)
	t.clock.mu.Unlock()
}

//============================================================================

/*
================
manualClockHub

A hub on a fresh manual clock, with its sessions' cleanup registered.
================
*/
func manualClockHub(t *testing.T, cfg Config) (*Hub, *manualClock) {
	t.Helper()
	clock := newManualClock()
	hub := newHub(cfg)
	clock.install(hub)
	return hub, clock
}
