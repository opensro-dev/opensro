/*
===========================================================================

spiritability.go - the worn Devil's Spirit's abilities, on and off

Port-only, not native to v1.150 (#3456; SRO_NASRUN opens the Spirit's
socket, NasrunSocket). While a Spirit is worn in avatar slot 4 and its
clock is awake, its codename and plus grant what nasrun.SpiritAbility
names: skills to cast, HP/MP percents and a block rate. This owner keeps
AvatarInventory.Spirit, the runtime record combat and the learned-skill
check read, true to the row and the clock:

  - an avatar move derives it on the next snapshot (avatartransfer.go);
  - an entering session derives it before its stats are first sent;
  - the clock's end clears it from the action tick (advanceSpirits), as
    the v1.188 server's avatar expiry pass does (it removes the item's
    contributions, detaches its plus abilities, then refreshes).

That pass also sends 0x3078 {3, slot}. The v1.150 client has no 0x3078
handler (no code names the opcode), so the port sends the stats refresh
alone. Inferred: when the Spirit comes off or lapses, any running buff
whose reqi the equipment no longer meets ends, as after an equipment move
(equipmentreqi.go); the reference pass removes the item's contributions
and the reqi re-evaluation is the server's rule for every equipment loss.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/nasrun"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
spiritExpiry

One index entry: the character whose attached Spirit lapses at
untilUnix. The character store owns the state; this is only the tick's
lookup, as berserkActors is.
================
*/
type spiritExpiry struct {
	division, name string
	untilUnix      int64
}

/*
================
deriveSpirit

The Spirit effect avatars give at nowUnix, or nil: the socket must be
open, slot 4 must hold an awake Spirit, and its codename and plus must
have an ability. A granted skill the skill data lacks (a variant built
without the skill import) is left out; the stats still apply.
================
*/
func (rt *Runtime) deriveSpirit(avatars *domain.AvatarInventory, nowUnix int64) *domain.SpiritEffect {
	if !rt.NasrunSocket || avatars == nil {
		return nil
	}
	for _, row := range avatars.Rows {
		if row.Slot != int64(nasrun.SpiritSlot) || !nasrun.IsSpirit(row.TypeFlags) || !nasrun.Awake(row.Data, nowUnix) {
			continue
		}
		ability, ok := nasrun.SpiritAbility(row.Codename, row.Plus)
		if !ok {
			return nil
		}
		effect := &domain.SpiritEffect{Slot: row.Slot, Plus: row.Plus,
			HPPercent: ability.HPPercent, MPPercent: ability.MPPercent, Block: ability.Block}
		for _, codename := range ability.Skills {
			if skill, found := rt.skillByCodename(codename); found {
				effect.Skills = append(effect.Skills, skill.ID)
			}
		}
		return effect
	}
	return nil
}

/*
================
spiritExpiryOf

The instant the attached Spirit in avatars lapses, or 0 when none is
attached.
================
*/
func spiritExpiryOf(avatars *domain.AvatarInventory) int64 {
	if avatars == nil || avatars.Spirit == nil {
		return 0
	}
	for _, row := range avatars.Rows {
		if row.Slot == avatars.Spirit.Slot {
			return int64(row.Data)
		}
	}
	return 0
}

/*
================
indexSpirit

Points the tick's index at c's attached Spirit, or drops c from it.
================
*/
func (rt *Runtime) indexSpirit(division string, c *enterworld.Character) {
	key := simulation.WorldKey(division, c.Name)
	if until := spiritExpiryOf(c.AvatarInventory); until != 0 {
		rt.spiritActors.Store(key, spiritExpiry{division, c.Name, until})
		return
	}
	rt.spiritActors.Delete(key)
}

/*
================
spiritDetachFrames

The frames for a Spirit that came off or lapsed while buffs leaned on
it: the ended instances (public) and the owner's gauges and stats.
The caller holds c's door.
================
*/
func (rt *Runtime) spiritDetachFrames(division string, c *enterworld.Character, nowMs int64) (public, actor []wire.Frame) {
	ended := rt.retireUnmetEquipmentEffects(division, c)
	return rt.finishEndedEffects(division, c, ended, nowMs)
}

/*
================
restoreSpirit

An entering session's Spirit: derived before the first stats are sent,
so the entry projection already counts it.
================
*/
func (rt *Runtime) restoreSpirit(division, name string) {
	c := rt.findCharacter(division, name)
	if c == nil || c.AvatarInventory == nil {
		return
	}
	now := rt.Now().Unix()
	rt.deps.UpdateDeferred(c, "spirit-restore", func() bool {
		c.AvatarInventory.Spirit = rt.deriveSpirit(c.AvatarInventory, now)
		return true
	})
	rt.indexSpirit(division, c)
}

/*
================
advanceSpirits

The action tick's lapse pass: every attached Spirit whose clock has
run out comes off, in name order.
================
*/
func (rt *Runtime) advanceSpirits(nowMs int64) []simulation.DivisionFrames {
	nowUnix := nowMs / 1000
	var keys []string
	rt.spiritActors.Range(func(k, v any) bool {
		if v.(spiritExpiry).untilUnix <= nowUnix {
			keys = append(keys, k.(string))
		}
		return true
	})
	sort.Strings(keys)
	var out []simulation.DivisionFrames
	for _, key := range keys {
		value, ok := rt.spiritActors.Load(key)
		if !ok {
			continue
		}
		job := value.(spiritExpiry)
		sourceGID, public, actor := rt.expireSpiritInDivision(key, job, nowMs)
		if len(public) != 0 {
			batch := simulation.DivisionFrames{DivisionID: job.division, SourceGID: sourceGID}
			for _, f := range public {
				batch.Frames = append(batch.Frames, simulation.Frame{Opcode: f.Opcode, Payload: f.Payload})
			}
			out = append(out, batch)
		}
		if len(actor) != 0 && rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(job.division, job.name, actor)
		}
	}
	return out
}

/*
================
expireSpiritInDivision

Clears one lapsed Spirit under its division's door. A Spirit extended
or replaced since the index was written is left alone; the index then
follows it again.
================
*/
func (rt *Runtime) expireSpiritInDivision(key string, job spiritExpiry, nowMs int64) (sourceGID uint32, public, actor []wire.Frame) {
	unlock := rt.lockDivision(job.division)
	defer unlock()
	c := rt.findCharacter(job.division, job.name)
	if c == nil {
		rt.spiritActors.CompareAndDelete(key, job)
		return 0, nil, nil
	}
	sourceGID = enterworld.ObjectIDForCharacter(c)
	rt.deps.UpdateDeferred(c, "spirit-lapse", func() bool {
		if c.AvatarInventory == nil || c.AvatarInventory.Spirit == nil {
			return false
		}
		next := rt.deriveSpirit(c.AvatarInventory, nowMs/1000)
		if next != nil {
			c.AvatarInventory.Spirit = next
			return true
		}
		c.AvatarInventory.Spirit = nil
		public, actor = rt.spiritDetachFrames(job.division, c, nowMs)
		if len(actor) == 0 {
			// No ended buff refreshed the stats; the Spirit's own share
			// still left them.
			hp, mp := rt.clampStoredGaugeToKeeper(job.division, c)
			actor = rt.gaugeDropFrames(job.division, c, hp, mp, true)
		}
		// The lapsed Spirit's skills leave the owner's strip (#4384).
		actor = append(actor, wire.SpiritSkillsFrame(nil))
		return true
	})
	rt.spiritActors.CompareAndDelete(key, job)
	rt.indexSpirit(job.division, c)
	return sourceGID, public, actor
}

/*
================
spiritSkills

The skills the avatar storage's derived Spirit grants, or none: what the
owner's skill strip shows (wire.OpSpiritSkills).
================
*/
func spiritSkills(avatars *domain.AvatarInventory) []uint32 {
	if avatars == nil || avatars.Spirit == nil {
		return nil
	}
	return avatars.Spirit.Skills
}

/*
================
SpiritSkillsAt

The skills the character's worn, awake Spirit lends now, derived from its
stored avatar rows: the entry bootstrap names them (#4384).
================
*/
func (rt *Runtime) SpiritSkillsAt(_ string, c *enterworld.Character) []uint32 {
	if c == nil {
		return nil
	}
	effect := rt.deriveSpirit(c.AvatarInventory, rt.Now().Unix())
	if effect == nil {
		return nil
	}
	return append([]uint32(nil), effect.Skills...)
}
