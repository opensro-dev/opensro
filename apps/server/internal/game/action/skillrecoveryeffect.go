/*
===========================================================================

skillrecoveryeffect.go - the one-shot effect an instant support cast shows

An instant heal, cure or resurrection changes vitals and states, but the
recipients only see it land because the server also sends each of them an
attached-effect add with instance token 0. The v1.150 client plays a
skill's ACT_S / ACT_L stages (Force Heal's water_hpheal_effect_a, Force
Cure's water_cure_effect_a) only for an attached effect (0xB419, 776450 ->
8DF220 -> 8DD6F0); token 0 makes 8DD6F0 run them once and extinguish the
decoration at once (8DD110), so nothing is listed and no icon is shown.

The v1.188 server sends it from the tail of its skill-effect application:
it allocates 0xB0BD (the v1.188
number of the same add) with token 0 and
broadcasts it around each recipient. Heals over time are not here: their
timed handler (column 68 = 3, RefSkill+0x168) skips this path
and they install a real effect (skillhealtime.go).

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// recoveryEffectToken is the instance token the native effect add receives: none.
const recoveryEffectToken = 0

/*
==================
recoveryEffectRecipients

Who gets the add, as the native effect tail chooses from its action vector
(skillCureVector):

An empty vector names the caster when the row has no explicit target
(the native byte +0x94 test is column 22, TargetRequired), unless the
row resurrects (+0x330). The tant / tnt2 pair (+0x3C8 / +0x3CC)
that also skips it is on no heal, cure or resu row.

Otherwise every entry gets one: a living one (vfunc F8 == 1), or for a
resurrection a dead one. The dtnt skip (+0x3D0) is on no support
row either.

A heal whose efr is shape 6 (Mana Wind, Healing Division) reads efr word 1
and, with a non-empty vector, leaves its loop
past the tail: it sends nothing. Its vector is the native party selection's
party list (select 4 or 5), not skillCureVector's, which keeps shape 6 out
of the cure; only an empty party list reaches the caster rule above.

A companion in the vector (a cured pet) exists only while it lives, so a
resolved one counts as living; a resurrection never selects one.
==================
*/
func (rt *Runtime) recoveryEffectRecipients(division string, caster *enterworld.Character, skill enterworld.SkillRow, vector []uint32, now int64) []uint32 {
	resu := skill.Abnormal.AdmitDeadParty
	area := skill.Abnormal.EffectArea
	if skill.Heal.Present && area.Present && area.Shape == healSecondaryShape {
		vector = rt.partyCureTargets(division, caster, area.Radius, area.Select&1 != 0, resu, now)
		if len(vector) != 0 {
			return nil
		}
	}
	if len(vector) == 0 {
		if skill.TargetRequired || resu {
			return nil
		}
		return []uint32{enterworld.ObjectIDForCharacter(caster)}
	}

	var out []uint32
	for _, gid := range vector {
		player := rt.characterSnapshot(division, rt.findCharacterByGid(division, gid))
		if player == nil {
			if !resu && rt.characterByCosGID(division, gid) != nil {
				out = append(out, gid)
			}
			continue
		}
		if enterworld.CharacterAlive(player) != resu {
			out = append(out, gid)
		}
	}
	return out
}

/*
==================
recoveryEffectRoutes

One token-0 add per recipient, in vector order, each published around its
recipient (the native nearby broadcast). The status byte is written only for a row
whose layout carries it, defaulting to 2 as 776450 does; the RPBU / STDU /
DTDR word only for a row that has one, and it is zero.
==================
*/
func recoveryEffectRoutes(skill enterworld.SkillRow, recipients []uint32) []ObservedFrames {
	var out []ObservedFrames
	for _, gid := range recipients {
		payload, err := (wire.AttachedEffect{
			GID:           gid,
			SkillID:       skill.ID,
			InstanceToken: recoveryEffectToken,
			Phase:         2,
		}).Encode(wire.AttachedEffectLayout{Status: skill.SpawnStatus, Rider: skill.EffectRider})
		if err != nil {
			continue
		}
		out = append(out, ObservedFrames{SourceGID: gid, Frames: []wire.Frame{{Opcode: wire.OpAttachedEffect, Payload: payload}}})
	}
	return out
}
