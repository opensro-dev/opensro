/*
===========================================================================

monstervitals.go - a monster's vitals to the players that hold it

The reference server never broadcasts a monster's HP. Once per tick, for a
monster whose HP or status moved, its vitals publisher broadcasts the
status alone (flags 4) and hands the change to the relations that hold the
monster with the vitals bit: each of their players receives a private
0x33A6 with the cause word, the HP and the status. The monster's cause
words and that payload are MonsterState's (simulation/monster_vitals.go);
this lane finds the holders.

A player holds a monster through the first of the seven relation slots a
player owns, bound with the vitals bit. Only the object select (v1.150
0x745A) binds a monster there: it empties the slot and binds the selected
character with range 910; the selection plane (select.go) is that slot.
The browser client selects on every click on a monster, so a player
attacks what it holds. A cast binds nothing of its own; the one cast-side
bind is Scorn's forced target, which binds the victim's slot to the caster
(skillforcedtarget.go). Selecting anything else or releasing the target
leaves the monster.

The slot's range is judged only when the held monster moves to another
navigation cell: a holder farther than the range then loses the hold in
silence (retireDistantHolders). The vitals publication itself never
measures it.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// monsterHoldRange is the range the object select gives the relation that
// holds a monster (910; other objects get 320).
const monsterHoldRange float32 = 910

/*
================
vitalsTarget

A changed monster, as a holder's selection names it: its division and gid.
================
*/
type vitalsTarget struct {
	division string
	gid      uint32
}

/*
================
publishMonsterVitals

The action tick's share of the reference server's per-object tick: every
monster whose vitals changed since the last tick sends its private 0x33A6
to each player that holds it, then forgets the change. A changed monster nobody holds sends
nothing. Runs after every step that can change a monster's HP or status,
so a change made in this tick leaves in it.
================
*/
func (rt *Runtime) publishMonsterVitals(nowMs int64) []simulation.DivisionFrames {
	if rt.Monsters == nil || rt.Selected == nil {
		return nil
	}
	rt.monsterVitals = rt.Monsters.DrainChangedVitals(rt.monsterVitals[:0])
	if len(rt.monsterVitals) == 0 {
		return nil
	}
	if rt.vitalsTargets == nil {
		rt.vitalsTargets = make(map[vitalsTarget]int)
	}
	clear(rt.vitalsTargets)
	for index, changed := range rt.monsterVitals {
		rt.vitalsTargets[vitalsTarget{division: changed.DivisionID, gid: changed.Gid}] = index
	}
	rt.vitalsHolders = rt.Selected.holders(rt.vitalsHolders[:0])
	var out []simulation.DivisionFrames
	for _, holder := range rt.vitalsHolders {
		index, held := rt.vitalsTargets[vitalsTarget{division: holder.division, gid: holder.gid}]
		if !held {
			continue
		}
		characterID := rt.vitalsHolderID(holder)
		if characterID == 0 {
			continue
		}
		out = append(out, simulation.DivisionFrames{
			DivisionID:      holder.division,
			OnlyCharacterID: characterID,
			Frames:          []simulation.Frame{{Opcode: simulation.OpVitalsUpdate, Payload: rt.monsterVitals[index].Payload}},
		})
	}
	// The selection plane is a map: order the private routes by their
	// recipient, so a tick's output does not depend on its walk.
	sort.Slice(out, func(i, j int) bool { return out[i].OnlyCharacterID < out[j].OnlyCharacterID })
	return out
}

/*
================
vitalsHolderID

The character id a holder's private frame routes to, or 0 once the
character left the division.
================
*/
func (rt *Runtime) vitalsHolderID(holder selectionHolder) int64 {
	unlock := rt.lockDivision(holder.division)
	defer unlock()
	c := rt.findCharacter(holder.division, holder.name)
	if c == nil {
		return 0
	}
	return c.ID
}

/*
================
retireDistantHolders

The reference server measures a relation's range only when the object it
holds moves to another navigation cell: every relation that holds the
object with the vitals bit and a range is measured from its player (the
3D actor distance), and one whose range is below the distance is detached
in silence; the player's client keeps its target ring and stops receiving
the HP until it selects again. A resting monster, and a player walking
away from one, keep the hold.

The port keeps no navigation cells. A monster in flight is taken to cross
one on every tick (inference: a walking monster crosses cells
continuously, and the measure is the same at each of them), so its
holders are measured each tick while it moves. Only monster holds are
judged: no other hold changes what a player receives.
================
*/
func (rt *Runtime) retireDistantHolders(nowMs int64) {
	if rt.Monsters == nil || rt.Selected == nil {
		return
	}
	rt.vitalsHolders = rt.Selected.holders(rt.vitalsHolders[:0])
	for _, holder := range rt.vitalsHolders {
		if holder.gid < domain.MonsterGIDBase || holder.gid > domain.MonsterGIDLimit {
			continue
		}
		mover, ok := rt.Monsters.Mover(holder.division, holder.gid)
		if !ok || !mover.InFlight(nowMs) {
			continue
		}
		from, ok := rt.holderPose(holder, nowMs)
		if !ok {
			continue
		}
		if monsterHoldRange < monster.NativeActorDistance(from, mover.LivePoseAt(nowMs, nil)) {
			rt.Selected.release(holder.division, holder.name, holder.gid)
		}
	}
}

/*
================
holderPose

The holder's live position, or false once the character left the
division.
================
*/
func (rt *Runtime) holderPose(holder selectionHolder, nowMs int64) (monster.Pose, bool) {
	unlock := rt.lockDivision(holder.division)
	defer unlock()
	c := rt.findCharacter(holder.division, holder.name)
	if c == nil {
		return monster.Pose{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(holder.division, c.Name), c, nowMs)
	return monster.Pose{RegionID: at.RegionID, X: at.X, Y: at.Y, Z: at.Z}, true
}
