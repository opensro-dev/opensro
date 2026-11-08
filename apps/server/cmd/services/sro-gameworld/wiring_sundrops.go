/*
===========================================================================

wiring_sundrops.go - the Sun drop counting cycle, installed (#2538)

Port-only, not native, and count-only: with SRO_SUN_DROP_COUNT off (the
default) nothing is installed and nothing counts. On, the action
runtime's Sun sink points at a counter that names every counted Sun
drop in the log - item, recipient and time - so one measurement cycle
can total the shard's Suns per month and who received them from the log
alone, before anyone considers a Sun knob (decision record on #2538).

===========================================================================
*/
package main

import (
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/action"
)

/*
================
sunDropCounter

In-memory totals behind a short lock: NoteSunDrop runs inside the loot
settlement's store door, so it may only touch memory and log.
================
*/
type sunDropCounter struct {
	mu          sync.Mutex
	total       int
	byCharacter map[int64]int
}

/*
================
NoteSunDrop

Counts one Sun drop for its recipient and names it in the log. Invalid
identities are refused silently: the settlement path must never fail
over a count.
================
*/
func (c *sunDropCounter) NoteSunDrop(characterID int64, codename string, at time.Time) {
	if c == nil || characterID <= 0 || codename == "" {
		return
	}
	c.mu.Lock()
	c.total++
	total := c.total
	c.byCharacter[characterID]++
	c.mu.Unlock()
	log.Infof("sun drop count: %s to character %d at %s (%d counted this run)", codename, characterID, at.UTC().Format(time.RFC3339), total)
}

/*
================
installSunDropCount

Points the action runtime's Sun sink at the counter. Nothing is opened
and nothing persists: the log is the cycle's record.
================
*/
func installSunDropCount(gameplay *gameplayPlane) {
	if !action.SunDropCountFromEnv() {
		return
	}
	gameplay.items.SunDrops = &sunDropCounter{byCharacter: map[int64]int{}}
	log.Infof("sun drop count: counting ON (%s); every counted Sun drop is named in this log", action.EnvSunDropCount)
}
