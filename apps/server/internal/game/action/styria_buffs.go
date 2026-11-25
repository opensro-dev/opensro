/*
===========================================================================

styria_buffs.go - Styria Clash: the five arena blessings

Port-only, not native (epic #3648, slice 3b). The later client's own
data carries the five arena buffs as skills 34622-34626
(SKILL_ARENA_JOB_WAR_*_BUFF), 120 seconds each: damage +30%, damage
absorb 30%, HP and MP +1000, hit rate +20%, parry +20% (slice 3's
evidence on #3648). No table the port ships carries those rows and
the later client's skilldata does not carry them either (scanned:
only the arena's region names), so the five rows live here as data,
in the family's order, built on the native buff blocks the effect
installer already runs (dru, odar, the timed-item hpi/mpi lane, hr,
er).

PORT CHOICE, recorded because no source carries the grant rule: a
treasure-box opening earns the opener the side's next blessing in
the family's order, both sides cycling independently; a sixth
opening starts the order over (the effect owner refreshes the
instance). Kills score but grant nothing - the treasure is the
blessing's source, as the boxes are the match's stated prize. A
blessing ends on its 120 seconds, and early when its holder leaves
the match (death, logout, the clock's end): it is an arena blessing,
never open-field equipment.

Plain-English notices until the client slice binds the arena
strings, as in styria_score.go.

===========================================================================
*/

package action

import (
	"fmt"
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

// The blessing family (slice 3's evidence): skill ids in grant order.
const (
	styriaBlessingFirstID uint32 = 34622
	styriaBlessingLastID  uint32 = 34626
	styriaBlessingMs      int64  = 120 * 1000
)

// styriaBlessing is one row of the family: its skill identity, the
// words the match hears, and the native blocks it installs.
type styriaBlessing struct {
	id       uint32
	codename string
	name     string
	effect   string
	build    func(row *enterworld.SkillRow)
}

// styriaBlessings is the family in grant order: damage, absorption,
// vitality, accuracy, parry.
var styriaBlessings = []styriaBlessing{
	{styriaBlessingFirstID, "SKILL_ARENA_JOB_WAR_DAMAGE_BUFF", "striking", "damage +30%",
		func(row *enterworld.SkillRow) {
			row.BuffModifiers.Dru = true
			row.BuffModifiers.DruWords = [2]uint32{30, 30}
		}},
	{styriaBlessingFirstID + 1, "SKILL_ARENA_JOB_WAR_ABSORB_BUFF", "warding", "damage taken -30%",
		func(row *enterworld.SkillRow) {
			// odar 0xF: both lanes, basic and skill alike (the
			// parser's fix-up of an authored both-lanes word).
			row.BuffModifiers.Odar = true
			row.BuffModifiers.OdarBits = 0xF
			row.BuffModifiers.OdarWord = 30
			row.TimedEffect.IncomingReduction = true
		}},
	{styriaBlessingFirstID + 2, "SKILL_ARENA_JOB_WAR_VITALITY_BUFF", "vitality", "HP and MP +1000",
		func(row *enterworld.SkillRow) {
			// hpi/mpi flats install through the timed-item lane
			// alone (timeditemmodifier.go), so this one row is an
			// item program; it is not persistent, so no timed job
			// survives the match on it.
			row.TimedEffect.ItemProgram = true
			row.TimedEffect.HP = enterworld.SkillFlatRate{Present: true, Flat: 1000}
			row.TimedEffect.MP = enterworld.SkillFlatRate{Present: true, Flat: 1000}
		}},
	{styriaBlessingFirstID + 3, "SKILL_ARENA_JOB_WAR_ACCURACY_BUFF", "accuracy", "hit rate +20%",
		func(row *enterworld.SkillRow) {
			row.BuffModifiers.Hr = true
			row.BuffModifiers.HrRate = 20
			row.TimedEffect.HitRate = true
		}},
	{styriaBlessingFirstID + 4, "SKILL_ARENA_JOB_WAR_PARRY_BUFF", "parrying", "parry +20%",
		func(row *enterworld.SkillRow) {
			row.BuffModifiers.Er = true
			row.BuffModifiers.ErRate = 20
			row.TimedEffect.Parry = true
		}},
}

/*
================
styriaBlessingRow

One blessing as the skill row the effect owner installs: a
two-minute, un-cancellable timed effect whose group is its own
skill, so the five coexist (a re-grant retires the living instance
first, in styriaGrantBlessing). Group = the skill id itself (the
shipped rows group chain ranks; a blessing has no chain).
================
*/
func styriaBlessingRow(index int) enterworld.SkillRow {
	blessing := styriaBlessings[index]
	row := enterworld.SkillRow{
		ID:                     blessing.id,
		Codename:               blessing.codename,
		Group:                  blessing.id,
		EffectDurationMs:       uint32(styriaBlessingMs),
		EffectDurationPresent:  true,
		VoluntaryCancelBlocked: true,
	}
	row.TimedEffect.Pinned = true
	blessing.build(&row)
	return row
}

/*
================
styriaBlessingIndex

The family's index of a skill id, or -1 for any other skill.
================
*/
func styriaBlessingIndex(skillID uint32) int {
	if skillID < styriaBlessingFirstID || skillID > styriaBlessingLastID {
		return -1
	}
	return int(skillID - styriaBlessingFirstID)
}

/*
================
styriaGrantBlessing

Install one blessing on its opener through the shared effect owner
(the fortress rank's door shape: the deferred character door, the
owner's frames to the holder, the public half to the division's
peers). Best-effort by design: the banked point stands whatever the
install does, so a failure reads as no blessing, never a lost score.
================
*/
func (rt *Runtime) styriaGrantBlessing(division string, character *enterworld.Character, index int, nowMs int64) bool {
	if rt.effects == nil || character == nil || index < 0 || index >= len(styriaBlessings) {
		return false
	}
	row := styriaBlessingRow(index)
	var public []wire.Frame
	granted := false
	rt.deps.UpdateDeferred(character, "styria-blessing", func() bool {
		// A re-grant replaces, never stacks: the cast lane retires a
		// living instance of the same skill before its own commit,
		// and this server commit does the same by hand.
		for _, effect := range rt.effects.Snapshot(division, character.Name) {
			if effect.SkillID == row.ID {
				ended, _ := rt.finishEndedEffects(division, character,
					rt.effects.RetireInstances(division, character.Name,
						[]uint32{effect.InstanceToken}), nowMs)
				public = append(public, ended...)
			}
		}
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		if token == 0 {
			token = atomic.AddUint32(&rt.castTokenCounter, 1)
		}
		frames, ok := rt.commitCharacterEffect(division, character, row, token,
			statuseffect.StateActive, false, EffectPresentation{Phase: 2}, nowMs)
		if !ok {
			return false
		}
		public = append(public, frames...)
		granted = true
		return true
	})
	if !granted {
		return false
	}
	if rt.PushCharacterFrames != nil {
		private := append(append([]wire.Frame{}, public...),
			rt.gaugeDropFrames(division, character, true, true, true)...)
		rt.PushCharacterFrames(division, character.Name, private)
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, character.Name, public)
	}
	return true
}

/*
================
styriaRetireBlessings

End a departed holder's blessings at once (the honor buffs'
reconcile shape): a blessing never leaves the arena on its holder.
Retiring nothing is not a write.
================
*/
func (rt *Runtime) styriaRetireBlessings(division string, character *enterworld.Character, nowMs int64) {
	if rt.effects == nil || character == nil {
		return
	}
	var public, actor []wire.Frame
	rt.deps.UpdateDeferred(character, "styria-blessing-retire", func() bool {
		var tokens []uint32
		for _, effect := range rt.effects.Snapshot(division, character.Name) {
			if styriaBlessingIndex(effect.SkillID) >= 0 {
				tokens = append(tokens, effect.InstanceToken)
			}
		}
		if len(tokens) == 0 {
			return false
		}
		public, actor = rt.finishEndedEffects(division, character,
			rt.effects.RetireInstances(division, character.Name, tokens), nowMs)
		return len(public) > 0 || len(actor) > 0
	})
	if len(public) == 0 && len(actor) == 0 {
		return
	}
	if rt.PushCharacterFrames != nil {
		rt.PushCharacterFrames(division, character.Name,
			append(append([]wire.Frame{}, public...), actor...))
	}
	if len(public) > 0 && rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, character.Name, public)
	}
}

/*
================
styriaBlessingText

The words the whole match hears when an opening earns a blessing.
================
*/
func styriaBlessingText(name string, index int) string {
	blessing := styriaBlessings[index]
	return fmt.Sprintf("Styria Clash: %s earns the blessing of %s (%s).",
		name, blessing.name, blessing.effect)
}
