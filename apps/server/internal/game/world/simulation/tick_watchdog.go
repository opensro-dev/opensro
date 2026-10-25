/*
===========================================================================

tick_watchdog.go - names the work behind a stalled simulation tick

A tick that runs for seconds freezes every client's replies at once (moves,
heartbeats and broadcasts arrive together when it ends), and the tick metric
can only say that it happened. The coordinator stamps the phase it is in and
the hook it is running; a watchdog goroutine reports a tick that outlives
the stall threshold exactly once, with every goroutine's stack, so the lock
or I/O holding the tick is in the log the first time it happens.

A tick that has made no progress for StallExit is a deadlock: Go locks
have no timeout and a parked goroutine cannot be killed, so nothing inside
the process can recover. Progress is a step counter the coordinator and
the division workers bump per division leg and per hook; a tick that is
merely slow (a host starved of CPU runs every worker late, and its stack
dump shows no lock waiters) keeps bumping it and is left to finish, up to
stallCeilingFactor times StallExit, so a runaway loop that still bumps a
counter cannot hold the world forever either. The watchdog then writes a dump file (the stalled phase and hook,
the goroutines waiting on locks, every stack) to StallDumpDir and exits
non-zero; the supervisor restarts the process. Every store operation
commits on its own, so the restart loses no committed state. This is an
operations safeguard, not a game rule; the dump is what fixes the bug.

The stamps are three atomic stores per hook. Nothing is formatted unless a
tick has already stalled.

===========================================================================
*/

package simulation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// DefaultTickStallReport is ten tick budgets: far past any overrun the
	// metric already counts, short enough to catch a stall while it lasts.
	DefaultTickStallReport = time.Second
	// TickStallUnready is how long a tick may run, or the world go without
	// finishing one, before readiness reports the world unready (#570).
	// Longer than the report, so a single slow tick only logs.
	TickStallUnready    = 5 * time.Second
	tickStallPoll       = 250 * time.Millisecond
	tickStallStackBytes = 1 << 20
	// tickStallDumpMaxBytes bounds the dump's stack buffer; it doubles from
	// tickStallStackBytes until every goroutine fits.
	tickStallDumpMaxBytes = 64 << 20
	// tickStallExitCode is the process status after a stall dump.
	tickStallExitCode = 3
	// stallCeilingFactor bounds a tick that keeps progressing: past this
	// many StallExit periods it is dumped and exited like a deadlock.
	stallCeilingFactor = 4
)

// EnvTickStallExit is the seconds one tick may stay stuck before the
// process dumps and exits; "0" never exits. Unset uses DefaultTickStallExit.
const EnvTickStallExit = "SRO_TICK_STALL_EXIT"

// EnvTickStallDumpDir overrides where stall dumps are written.
const EnvTickStallDumpDir = "SRO_TICK_STALL_DUMP_DIR"

// DefaultTickStallExit is thirty report thresholds: no healthy tick comes
// near it, and players wait at most this long before the restart.
const DefaultTickStallExit = 30 * time.Second

// Phases of one coordinated tick, in execution order.
const (
	tickPhaseIdle int32 = iota
	tickPhaseBeforeHooks
	tickPhaseDivisions
	tickPhaseHooks
)

var tickPhaseNames = [...]string{"idle", "before-hooks", "divisions", "hooks"}

/*
================
tickWatch

Written by the coordinator, read by the watchdog. startNs is zero between
ticks; reported holds the startNs of the last tick already reported, so a
stall logs once however long it lasts.
================
*/
type tickWatch struct {
	startNs  atomic.Int64
	phase    atomic.Int32
	hook     atomic.Uintptr
	reported atomic.Int64
	// endNs is when the last tick finished; zero until the first one has.
	endNs atomic.Int64
	// steps counts finished units of tick work (division legs, hooks).
	steps atomic.Int64

	// Owned by the watchdog goroutine: the tick it last looked at, the
	// step count it saw, when that count last changed, and the tick it
	// already logged as slow.
	seenStart int64
	seenSteps int64
	seenAt    int64
	slowStart int64
}

/*
================
tickWatch.begin
================
*/
func (w *tickWatch) begin(now time.Time) {
	w.hook.Store(0)
	w.phase.Store(tickPhaseBeforeHooks)
	w.startNs.Store(now.UnixNano())
}

/*
================
tickWatch.end
================
*/
func (w *tickWatch) end(now time.Time) {
	w.endNs.Store(now.UnixNano())
	w.startNs.Store(0)
	w.phase.Store(tickPhaseIdle)
	w.hook.Store(0)
}

/*
================
tickWatch.enterHook

The hook's code pointer names it later; closures resolve to their
enclosing constructor (for example action.(*Runtime).TickHook.func1).
================
*/
func (w *tickWatch) enterHook(hook TickHook) {
	w.steps.Add(1)
	w.hook.Store(reflect.ValueOf(hook).Pointer())
}

/*
================
tickWatch.leaveHook
================
*/
func (w *tickWatch) leaveHook() {
	w.hook.Store(0)
	w.steps.Add(1)
}

/*
================
tickWatch.step

One unit of tick work finished. Safe from any shard worker.
================
*/
func (w *tickWatch) step() {
	w.steps.Add(1)
}

/*
================
tickWatch.stuckFor

How long the tick started at start has gone without a step at now. The
first look at a tick counts from its start, so a tick parked before the
watchdog first saw it is not given a fresh StallExit. Watchdog goroutine
only.
================
*/
func (w *tickWatch) stuckFor(start int64, now time.Time) time.Duration {
	steps := w.steps.Load()
	switch {
	case w.seenStart != start:
		w.seenStart, w.seenSteps, w.seenAt = start, steps, start
	case w.seenSteps != steps:
		w.seenSteps, w.seenAt = steps, now.UnixNano()
	}
	return time.Duration(now.UnixNano() - w.seenAt)
}

/*
================
tickWatch.stalled

How long the tick started at start has run at now, with its phase and
hook names.
================
*/
func (w *tickWatch) stalled(start int64, now time.Time) (time.Duration, string, string) {
	phase := w.phase.Load()
	name := "none"
	if phase >= 0 && int(phase) < len(tickPhaseNames) {
		name = tickPhaseNames[phase]
	}
	hook := "none"
	if pc := w.hook.Load(); pc != 0 {
		if fn := runtime.FuncForPC(pc); fn != nil {
			hook = fn.Name()
		}
	}
	return time.Duration(now.UnixNano() - start), name, hook
}

/*
================
tickWatch.check

Returns the report for a tick running longer than after at now, once per
tick, or "" when there is nothing to report.
================
*/
func (w *tickWatch) check(now time.Time, after time.Duration) string {
	start := w.startNs.Load()
	if start == 0 {
		return ""
	}
	elapsed, phase, hook := w.stalled(start, now)
	if elapsed < after || w.reported.Load() == start {
		return ""
	}
	w.reported.Store(start)
	stacks := make([]byte, tickStallStackBytes)
	stacks = stacks[:runtime.Stack(stacks, true)]
	return fmt.Sprintf("simulation: tick stalled %v in %s (hook %s); goroutines:\n%s",
		elapsed.Round(time.Millisecond), phase, hook, stacks)
}

/*
================
allStacks

Every goroutine's stack, growing the buffer until it fits.
================
*/
func allStacks() []byte {
	for size := tickStallStackBytes; ; size *= 2 {
		buf := make([]byte, size)
		n := runtime.Stack(buf, true)
		if n < size || size >= tickStallDumpMaxBytes {
			return buf[:n]
		}
	}
}

/*
================
lockWaiters

The goroutines parked on a sync lock, each as its header and its frames
from this module, innermost first: the lock graph of a deadlock at a
glance, ahead of the full stacks.
================
*/
func lockWaiters(stacks []byte) string {
	var b strings.Builder
	for _, g := range strings.Split(string(stacks), "\n\n") {
		lines := strings.Split(g, "\n")
		if !strings.Contains(lines[0], "[sync.") {
			continue
		}
		b.WriteString(lines[0])
		b.WriteByte('\n')
		for _, l := range lines[1:] {
			if !strings.HasPrefix(l, "opensro.online/") && !strings.HasPrefix(l, "main.") {
				continue
			}
			if i := strings.LastIndexByte(l, '('); i > 0 {
				l = l[:i]
			}
			b.WriteString("    ")
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

/*
================
writeStallDump

The dump an operator or agent reads to fix the deadlock. It never blocks
the exit: a dump that cannot be written goes to the log instead.
================
*/
func writeStallDump(dir string, now time.Time, elapsed time.Duration, phase, hook string) string {
	stacks := allStacks()
	body := fmt.Sprintf("tick stall dump\nat:      %s\nstalled: %v\nphase:   %s\nhook:    %s\npid:     %d\n\n"+
		"== goroutines waiting on locks ==\n%s\n== all goroutines ==\n%s",
		now.UTC().Format(time.RFC3339Nano), elapsed.Round(time.Millisecond), phase, hook, os.Getpid(),
		lockWaiters(stacks), stacks)
	path := filepath.Join(dir, "tick-stall-"+now.UTC().Format("20060102T150405Z")+".txt")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Errorf("simulation: stall dump directory %s: %v\n%s", dir, err, body)
		return ""
	}
	if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
		log.Errorf("simulation: stall dump %s: %v\n%s", path, err, body)
		return ""
	}
	return path
}

/*
================
TickStallExitFromEnv

SRO_TICK_STALL_EXIT in whole seconds; "0" disables the exit. Unset or
malformed uses DefaultTickStallExit, so a typo never disables recovery.
================
*/
func TickStallExitFromEnv() time.Duration {
	text := strings.TrimSpace(os.Getenv(EnvTickStallExit))
	if text == "" {
		return DefaultTickStallExit
	}
	seconds, err := strconv.Atoi(text)
	if err != nil || seconds < 0 {
		log.Warnf("simulation: %s=%q is not whole seconds; using %v", EnvTickStallExit, text, DefaultTickStallExit)
		return DefaultTickStallExit
	}
	return time.Duration(seconds) * time.Second
}

/*
================
tickWatch.progress

nil while the simulation makes progress at now: no tick has run longer than
after, and one finished within after. Atomics only, so a stalled tick or a
contended lock cannot delay the answer.
================
*/
func (w *tickWatch) progress(now time.Time, after time.Duration) error {
	if start := w.startNs.Load(); start != 0 {
		if elapsed := time.Duration(now.UnixNano() - start); elapsed >= after {
			return fmt.Errorf("simulation tick running for %v", elapsed.Round(time.Millisecond))
		}
		return nil
	}
	end := w.endNs.Load()
	if end == 0 {
		return fmt.Errorf("simulation has not finished a tick")
	}
	if idle := time.Duration(now.UnixNano() - end); idle >= after {
		return fmt.Errorf("no simulation tick finished for %v", idle.Round(time.Millisecond))
	}
	return nil
}

/*
================
Progress

The readiness signal: nil while ticks keep finishing (TickStallUnready).
================
*/
func (t *Ticker) Progress(now time.Time) error {
	return t.watch.progress(now, TickStallUnready)
}

/*
================
watchStalls

The ticker's watchdog goroutine. It never touches tick state beyond the
atomics, so a stalled coordinator cannot stall the report or the exit.
================
*/
func (t *Ticker) watchStalls(ctx context.Context) {
	after := t.StallReport
	if after <= 0 {
		after = DefaultTickStallReport
	}
	poll := time.NewTicker(tickStallPoll)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-poll.C:
			if report := t.watch.check(now, after); report != "" {
				log.Warn(report)
			}
			if t.stallExpired(now) {
				return
			}
		}
	}
}

/*
================
stallExpired

Dumps and exits once the current tick has made no progress for StallExit,
or has run for stallCeilingFactor times StallExit however it progresses.
Returns true after the exit, which only returns when exit is stubbed.
================
*/
func (t *Ticker) stallExpired(now time.Time) bool {
	start := t.watch.startNs.Load()
	if t.StallExit <= 0 || start == 0 {
		return false
	}
	elapsed, phase, hook := t.watch.stalled(start, now)
	stuck := t.watch.stuckFor(start, now)
	cause := ""
	switch {
	case stuck >= t.StallExit:
		cause = "no progress, deadlock assumed"
	case elapsed >= stallCeilingFactor*t.StallExit:
		cause = "still progressing past the ceiling, runaway assumed"
	}
	if cause == "" {
		if elapsed >= t.StallExit && t.watch.slowStart != start {
			t.watch.slowStart = start
			log.Warnf("simulation: tick slow %v in %s (hook %s) but progressing; not restarting before %v",
				elapsed.Round(time.Millisecond), phase, hook, stallCeilingFactor*t.StallExit)
		}
		return false
	}
	path := writeStallDump(t.StallDumpDir, now, elapsed, phase, hook)
	log.Errorf("simulation: tick stuck %v in %s (hook %s): %s; dump %s; exiting for restart",
		elapsed.Round(time.Millisecond), phase, hook, cause, path)
	exit := t.exit
	if exit == nil {
		exit = os.Exit
	}
	exit(tickStallExitCode)
	return true
}
