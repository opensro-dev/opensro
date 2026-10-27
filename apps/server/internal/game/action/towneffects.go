/*
===========================================================================

towneffects.go - the head of the native town teleport, shared by every
port town relocation

The reference server routes every town teleport through one routine.
Before it resolves the appointed point and moves (or, in its other arm,
recovers in place), it stops the character's movement, settles PvP
hostility, and clears every abnormal state. The port's relocations each
stop movement in their own way; what they all lacked is the hostility
and abnormal head. applyTownTeleportHead is that head, in the native
order: PvP first, then the abnormal clear.

Hostility follows the native branches on the character's PvP state
(domain PVPState): state 1 erases this character from every aggression
list in the division that names it (records are one-sided), clears its
own list, and so resets its state; state 2 (the durable penalty) clears
only its own list and keeps the state. State frames go out for every
character whose state changed.

Call it only for a town teleport proper (return scroll completion,
rebirth to the appointed point, GM town moves, a gate's return-point
row): gate-to-gate destination travel and reverse returns are different
native moves and do not take this head. The caller owns the refusal
gates and the move; the head applies even when the move itself is then
skipped, as the native routine applies it before its world check.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
applyTownTeleportHead

Settle PvP hostility and clear abnormal states for a town teleport.
Returns the frames addressed to the teleporting character: its own PvP
state frame (actor and public, as the state tick publishes it) and the
abnormal publication of the clear. Opponents whose state changed are
published directly. Call with the division lock held.
================
*/
func (rt *Runtime) applyTownTeleportHead(division string, c *enterworld.Character) (actor, public []wire.Frame) {
	if c == nil {
		return nil, nil
	}
	switch c.PVPState() {
	case 1:
		// Hostility records are one-sided: an attacker retains a target
		// that never retained it back. Erasing only the records this
		// character holds would leave those standing, so scan the
		// division and erase this character from every list that names
		// it, publishing each state that changes.
		self := enterworld.ObjectIDForCharacter(c)
		for _, other := range rt.deps.CharactersForDivision(division) {
			if other == nil || other == c {
				continue
			}
			if _, ok := other.Aggressions[self]; !ok {
				continue
			}
			before := other.PVPState()
			delete(other.Aggressions, self)
			if before != other.PVPState() {
				rt.publishBodyStatus(division, other.Name, []wire.Frame{playerPVPStateFrame(other)})
			}
		}
		c.Aggressions = nil
		frame := playerPVPStateFrame(c)
		actor = append(actor, frame)
		public = append(public, frame)
	case 2:
		c.Aggressions = nil
	}
	if owner := rt.clearPlayerAbnormalInDoor(division, c, rt.Now().UnixMilli()); owner != nil {
		publication := rt.playerAbnormalPublication(division, c, owner)
		actor = append(actor, publication.actor...)
		public = append(public, publication.public...)
	}
	return actor, public
}

/*
================
publishTownTeleportHead

applyTownTeleportHead for flows that return no frame result (the GM
town moves): actor frames to the character, public frames to the
division, each once.
================
*/
func (rt *Runtime) publishTownTeleportHead(division string, c *enterworld.Character) {
	actor, public := rt.applyTownTeleportHead(division, c)
	if rt.PushCharacterFrames != nil && len(actor)+len(public) > 0 {
		rt.PushCharacterFrames(division, c.Name, append(actor, public...))
	}
	if rt.PushDivisionPeerFrames != nil && len(public) > 0 {
		rt.PushDivisionPeerFrames(division, c.Name, public)
	}
}
