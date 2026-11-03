package statuseffect

// relativeJobClock is the online-seconds branch of the server's native
// skill-keeper tick. Absolute epoch jobs have a different record mode and
// are not admitted by this clock. All accumulators are stored as native
// float32.
type relativeJobClock struct {
	remaining               uint32
	checkpoint, accrued     float32
	active, retiredByEffect bool
}

func (c *relativeJobClock) retire(reason int32) {
	if reason == 1 {
		c.active, c.retiredByEffect = false, true
	}
}

// advance returns the checkpoint and forced-retirement dispatch decisions.
// Native subtracts one 300-second interval per qualifying update, not a loop.
func (c *relativeJobClock) advance(delta float32) (checkpoint, forceRetirement bool) {
	c.checkpoint += delta
	c.accrued += delta
	if c.accrued > 1 {
		elapsed := uint32(c.accrued)
		if c.remaining <= elapsed {
			c.remaining, c.checkpoint, c.accrued, c.active = 0, 0, 0, false
		} else {
			c.remaining -= elapsed
			c.accrued -= float32(elapsed)
			if c.checkpoint >= 300 {
				c.checkpoint -= 300
				checkpoint = true
			}
		}
	}
	return checkpoint, !c.active && !c.retiredByEffect
}

// advanceJob moves the durable-job clock to nowMs and reports whether
// the row moved, so the per-tick sweeps can leave untouched owners'
// snapshot views standing.
func (e *Effect) advanceJob(nowMs int64) bool {
	if !e.jobClockPresent || nowMs <= e.jobUpdatedAtMs {
		return false
	}
	checkpoint, _ := e.jobClock.advance(float32(nowMs-e.jobUpdatedAtMs) / 1000)
	e.jobCheckpointDue = e.jobCheckpointDue || checkpoint
	e.jobUpdatedAtMs = nowMs
	return true
}

// PersistentRemainingMs is the durable record's whole-second value, not the
// visual millisecond countdown. Reading it cannot advance the live owner.
func (e Effect) PersistentRemainingMs(nowMs int64) uint32 {
	if e.jobClockPresent {
		e.advanceJob(nowMs)
		if !e.jobClock.active {
			return 0
		}
		return e.jobClock.remaining * 1000
	}
	if e.ExpiresAtMs <= nowMs {
		return 0
	}
	return uint32((e.ExpiresAtMs - nowMs + 999) / 1000 * 1000)
}
