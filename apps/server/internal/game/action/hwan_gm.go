/*
===========================================================================

hwan_gm.go - the Blue Berserk GM test tools: /SETBERSERK and /SETGAUGE

Port-only, not native. The gmcommand hub (gmcommand/gmcommand.go) calls these two
seats; the Blue level itself belongs to the quest owner (quest/hwan_level.go) and the
frames to hwan.go.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
SetGMBerserkLevel

The gmcommand port: the GM's /SETBERSERK name level. Port-only test tooling,
not native: it sets the named character's Blue Berserk level (0 to 6) for the
life of the shard process, tells that character the new level and its title,
and leaves the quests alone. Privilege is rechecked inside the store door, as
SetGMSpeed does.
================
*/
func (rt *Runtime) SetGMBerserkLevel(division, sender, name string, level uint8) bool {
	if rt == nil || rt.deps == nil || rt.SetHwanLevel == nil || level > 6 {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, sender)
	target := rt.findCharacter(division, name)
	if gm == nil || target == nil {
		return false
	}
	var frames []wire.Frame
	if !rt.deps.Update(target, "gm-set-berserk", func() bool {
		if target.DeletePending || !gm.GMPrivilege || !rt.SetHwanLevel(target, level) {
			return false
		}
		frames = []wire.Frame{HwanLevelFrame(enterworld.ObjectIDForCharacter(target), level)}
		if level > 0 {
			frames = append(frames, HwanTitleNotice(target, level))
		}
		return true
	}) {
		return false
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, target.Name, frames)
	}
	return true
}

/*
================
SetGMBerserkGauge

The gmcommand port: the GM's /SETGAUGE name balls. Port-only test tooling, not
native: the named character's Berserk gauge holds exactly balls (0 to 5) from
now on, whatever it held (a gauge cannot be raised while Berserk is active, as
native, so that is refused). The character is told the new count. Privilege is
rechecked inside the store door.
================
*/
func (rt *Runtime) SetGMBerserkGauge(division, sender, name string, balls uint8) bool {
	if rt == nil || rt.deps == nil || balls > 5 {
		return false
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	gm := rt.findCharacter(division, sender)
	target := rt.findCharacter(division, name)
	if gm == nil || target == nil {
		return false
	}
	var frame wire.Frame
	if !rt.deps.Update(target, "gm-set-gauge", func() bool {
		if target.DeletePending || !gm.GMPrivilege || target.NativeBodyStatus == 1 && balls > target.BerserkPoints {
			return false
		}
		target.BerserkPoints = balls
		frame = berserkPointsFrame(target, 0)
		return true
	}) {
		return false
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, target.Name, []wire.Frame{frame})
	}
	return true
}
