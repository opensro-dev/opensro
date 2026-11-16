/*
===========================================================================

muhan_change.go - the Muhan change NPCs: Talk becomes the paired form

Port-only, not native (#3349), and present only while SRO_MUHAN is on.
Each arena carries change NPCs (NPC_MUHAN_CHANGE_*) spawned from its
nests, one per arena world. The later server's _RefMuhanMonsterTransMask
pairs each one with a BUFF01 skill (msch word 7) and a monster form
(MOB_MUHAN_CHANGE_*); the import writes that pairing as
muhan-transforms.txt.

Neither binary shows the interaction: the later client has no npcchat
row for a change NPC, and v1.188's effect switch has no case for word 7.
INFERENCE, recorded on #3349: the NPC's one function is the change, so
selecting it offers Talk, and the Talk request casts the paired skill as
an indirect skill whose context carries the form, exactly as a filled
mask capsule casts its skill (transform.go). The form then fills the
transform block, and every end path a mask has ends it too.

This module owns the pairing table and the two hooks: the select answer
(select.go) and the Talk request (npcaction.go). The change NPC itself
stays the nest's entity in its own arena world: all eight arenas share
region 27091, so a static roster NPC would stand in every one of them.

===========================================================================
*/

package action

import (
	"fmt"
	"sync/atomic"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
muhanChange

One change NPC's pairing, resolved at install: the skill its Talk casts
and the form that skill becomes.
================
*/
type muhanChange struct {
	skill enterworld.SkillRow
	form  monster.MonsterRef
}

/*
================
ConfigureMuhanTransforms

Called once at startup, after monster wiring, with the pairing the boot
loaded (monster.LoadMuhanTransforms; nil while SRO_MUHAN is off, which
leaves the runtime native). Every row must resolve: the NPC and the form
in the monster template, the skill in the skill table carrying msch word
7. A row that does not refuses the boot, as the NPC and wave loaders do;
a change NPC that cannot change must not reach gameplay.
================
*/
func (rt *Runtime) ConfigureMuhanTransforms(rows []monster.MuhanTransform) error {
	if len(rows) == 0 {
		return nil
	}
	if rt.Monsters == nil {
		return fmt.Errorf("muhan transforms: no monster population")
	}
	source, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	if !ok {
		return fmt.Errorf("muhan transforms: the skill table has no codename lookup")
	}
	changes := make(map[string]muhanChange, len(rows))
	for _, row := range rows {
		if _, found := rt.Monsters.ReferenceByCodename(row.NpcCodename); !found {
			return fmt.Errorf("muhan transforms: npc %s is not in the population", row.NpcCodename)
		}
		form, found := rt.Monsters.ReferenceByCodename(row.MonsterCodename)
		if !found {
			return fmt.Errorf("muhan transforms: form %s is not in the population", row.MonsterCodename)
		}
		skill, found := source.SkillByCodename(row.SkillCodename)
		if !found {
			return fmt.Errorf("muhan transforms: skill %s is not in the skill table", row.SkillCodename)
		}
		if !skill.CastGate.MschPresent || skill.CastGate.MschMode != mschMuhanChange {
			return fmt.Errorf("muhan transforms: skill %s carries no msch word %d", row.SkillCodename, mschMuhanChange)
		}
		changes[row.NpcCodename] = muhanChange{skill: skill, form: form}
	}
	rt.muhanChanges = changes
	return nil
}

/*
================
muhanChangeAt

The pairing of the change NPC with this object id, when the id names a
live change NPC in the viewer's own world and visibility ring (the
monster lookup select uses). False while no pairing is installed.
================
*/
func (rt *Runtime) muhanChangeAt(division string, c *enterworld.Character, gid uint32) (muhanChange, monster.Instance, bool) {
	if len(rt.muhanChanges) == 0 || rt.Monsters == nil {
		return muhanChange{}, monster.Instance{}, false
	}
	instance, ok := rt.characterMonster(division, c, gid)
	if !ok {
		return muhanChange{}, monster.Instance{}, false
	}
	change, ok := rt.muhanChanges[instance.Ref.Codename]
	return change, instance, ok
}

/*
================
muhanChangeWithinRange

The native NPC hit range (npcHitRange), measured from the caster's live
pose to where the change NPC stands now.
================
*/
func (rt *Runtime) muhanChangeWithinRange(division string, c *enterworld.Character, instance monster.Instance) bool {
	now := rt.Now().UnixMilli()
	viewer := rt.liveSpawn(simulation.WorldKey(division, c.Name), c, now)
	station := monster.Pose{RegionID: instance.Spawn.RegionID, X: instance.Spawn.X, Y: instance.Spawn.Y, Z: instance.Spawn.Z}
	if live := rt.monsterSpawn(division, instance.Gid, now); live.RegionID != 0 {
		station = monster.Pose{RegionID: live.RegionID, X: live.X, Y: live.Y, Z: live.Z}
	}
	distance := monster.NativeActorDistance(
		monster.Pose{RegionID: viewer.RegionID, X: viewer.X, Y: viewer.Y, Z: viewer.Z},
		station,
	)
	return !(npcHitRange < distance)
}

/*
================
muhanChangeSelect

The select answer for a change NPC: the NPC select result offering
Talk, its one function, where any other nest entity answers with its
HP. Nil when the object is not a change NPC.
================
*/
func (rt *Runtime) muhanChangeSelect(division string, c *enterworld.Character, gid uint32) []wire.Frame {
	if _, _, ok := rt.muhanChangeAt(division, c, gid); !ok {
		return nil
	}
	return []wire.Frame{{
		Opcode:  wire.OpObjectSelectResult,
		Payload: wire.EncodeNpcObjectSelectResult(gid, simulation.NpcTalkFlagTalk, 0),
	}}
}

/*
================
handleMuhanChangeTalk

A function request at a change NPC. Only Talk is offered; it casts the
paired skill as the capsule does (useMonsterCapsule): the msch gate with
the form as the context RefObj, then the effect, then the hide ends.
A refusal answers the skill's 0xB070 error and the 0xB338 kind-2
refusal, so the client's pending function request always retires. The
caller holds the division lock and has checked the selection.
================
*/
func (rt *Runtime) handleMuhanChangeTalk(division string, c *enterworld.Character, change muhanChange, instance monster.Instance, mask uint32) ([]wire.Frame, string) {
	if mask != simulation.NpcTalkFlagTalk {
		return npcFunctionRefusal(npcFunctionNotOffered), fmt.Sprintf("change NPC %s offers only Talk, not 0x%X", instance.Ref.Codename, mask)
	}
	if !rt.muhanChangeWithinRange(division, c, instance) {
		return npcFunctionTooFar(), fmt.Sprintf("change NPC %s is beyond its interaction range", instance.Ref.Codename)
	}
	var frames []wire.Frame
	refusal := ""
	rt.deps.Update(c, "muhan-change", func() bool {
		now := rt.Now().UnixMilli()
		if code := rt.contextSkillAdmission(division, c, change.skill, now, nil, nil, admitExecution, admitContext{Indirect: true, TransformRef: change.form.RefObjID}); code != 0 {
			frames = append(offensiveRefusal(code).Frames, npcFunctionRefusal(npcFunctionNotOffered)...)
			refusal = fmt.Sprintf("change %s refused 0x%04X", change.skill.Codename, code)
			return false
		}
		token := atomic.AddUint32(&rt.castTokenCounter, 1)
		if token == 0 {
			token = atomic.AddUint32(&rt.castTokenCounter, 1)
		}
		applied, ok := rt.commitCharacterEffect(division, c, change.skill, token, statuseffect.StateActive, false, EffectPresentation{Phase: 2, TransformRefObjID: change.form.RefObjID}, now)
		if !ok {
			frames = npcFunctionRefusal(npcFunctionNotOffered)
			refusal = fmt.Sprintf("change %s did not install", change.skill.Codename)
			return false
		}
		rt.retireHide(division, c, now)
		frames = applied
		return true
	})
	if refusal != "" {
		return frames, refusal
	}
	if rt.PushDivisionPeerFrames != nil {
		rt.PushDivisionPeerFrames(division, c.Name, frames)
	}
	// The change is the NPC's whole function: the interaction closes on
	// the empty-object answer, as an NPC with no dialog does.
	return append(frames, npcFunctionRefusal(selectObjectNotFound)...), ""
}
