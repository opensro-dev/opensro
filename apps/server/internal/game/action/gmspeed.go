/*
===========================================================================

gmspeed.go - a GM's /SETSPEED

Port-only, not native (owner decision 2026-10-12): the v1.150 client has no
speed command (its GM console list ends at /VERIFY). A GM sets their own
movement speed as a percentage of the effective speed (100 = normal) with
/SETSPEED <percent>; a bare /SETSPEED resets it. The factor multiplies the
committed walk and run speeds last (refreshMovementEffects), so buffs,
transforms and body states keep stacking underneath. It lives in memory for
the session only: leaving the world forgets it. SRO_GM_SETSPEED off, the
default here, refuses the command as an unimplemented one; the GameWorld's
Nomad job turns it on.

===========================================================================
*/
package action

import (
	"os"
	"strings"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EnvGMSetSpeed turns /SETSPEED on ("on", "1", "true").
const EnvGMSetSpeed = "SRO_GM_SETSPEED"

// The accepted /SETSPEED range, in percent of the effective speed.
const (
	GMSpeedMinPercent    = 10
	GMSpeedMaxPercent    = 1000
	gmSpeedNormalPercent = 100
)

/*
================
GMSetSpeedFromEnv
================
*/
func GMSetSpeedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvGMSetSpeed))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
gmSpeedFactor

The GM's /SETSPEED factor, 1 when none is set.
================
*/
func (rt *Runtime) gmSpeedFactor(division, name string) float32 {
	if percent, ok := rt.gmSpeeds.Load(simulation.WorldKey(division, name)); ok {
		return float32(percent.(uint16)) / gmSpeedNormalPercent
	}
	return 1
}

/*
================
SetGMSpeed

Called by the authenticated GM dispatcher. Privilege is rechecked inside
the store door, as ToggleGMBodyStatus does. Only the GM's own on-foot speed
changes: a mount's speed belongs to its COS keeper.
================
*/
func (rt *Runtime) SetGMSpeed(division, name string, percent uint16) bool {
	if rt == nil || rt.deps == nil || !rt.GMSetSpeed {
		return false
	}
	if percent < GMSpeedMinPercent || percent > GMSpeedMaxPercent {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	key := simulation.WorldKey(division, c.Name)
	var frames []wire.Frame
	if !rt.deps.Update(c, "gm-set-speed", func() bool {
		if c.DeletePending || !c.GMPrivilege {
			return false
		}
		if percent == gmSpeedNormalPercent {
			rt.gmSpeeds.Delete(key)
		} else {
			rt.gmSpeeds.Store(key, percent)
		}
		frames = rt.refreshMovementEffects(division, c, rt.Now().UnixMilli())
		return true
	}) {
		return false
	}
	rt.publishBodyStatus(division, c.Name, frames)
	return true
}
