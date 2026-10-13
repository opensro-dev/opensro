/*
===========================================================================

development_follow.go - summon-follow development capabilities (gated)

===========================================================================
*/

package action

import (
	"fmt"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
DevelopmentSummonReference

DevelopmentSummonReference discovers a supported authored wave from the
shipped catalog. No fixture-owned list of child references or skill IDs.
Only the explicitly gated development composition calls these capabilities.
==================
*/
func (rt *Runtime) DevelopmentSummonReference() (monster.MonsterRef, error) {
	return rt.DevelopmentSummonReferenceByCodename("")
}

// A requested natural key narrows the same authored admission path. It does
// not override skills, child types, grades, tactics or fixture isolation.
func (rt *Runtime) DevelopmentSummonReferenceByCodename(codename string) (monster.MonsterRef, error) {
	refs := rt.Monsters.SpawnableRefs()
	if codename != "" {
		ref, ok := rt.Monsters.ReferenceByCodename(codename)
		if !ok {
			return monster.MonsterRef{}, fmt.Errorf("unknown or ambiguous unique reference")
		}
		refs = []monster.MonsterRef{ref}
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Level != refs[j].Level {
			return refs[i].Level < refs[j].Level
		}
		return refs[i].RefObjID < refs[j].RefObjID
	})
	for _, ref := range refs {
		if codename != "" && ref.Codename != codename {
			continue
		}
		instance := monster.Instance{Ref: ref}
		hp := instance.EffectiveMaxHP()
		if hp == 0 {
			continue
		}
		instance.CurrentHP = hp - hp/10 - 1
		instance.DamageSinceSummon = hp/10 + 1
		plan, ok := rt.monsterSummonPlan(instance, 0)
		if !ok {
			continue
		}
		row, _ := rt.deps.SkillData().SkillByID(plan.SkillID)
		supported := true
		for _, entry := range row.Summon.Entries {
			if entry.RefObjID == 0 {
				continue
			}
			child, found := rt.Monsters.Reference(entry.RefObjID)
			if !found {
				supported = false
				break
			}
			if _, found = monster.ResolveSummonTactics(child, entry.Grade, func() float64 { return 0 }); !found {
				supported = false
				break
			}
		}
		if supported {
			return ref, nil
		}
	}
	return monster.MonsterRef{}, fmt.Errorf("no supported authored summon wave")
}

// The fixture seeds accumulated damage, then uses the real summon action and
// its casting/release/finalize schedule. It does not create children or packets.
func (rt *Runtime) DevelopmentStartSummon(division string, gid uint32) (uint32, error) {
	return rt.DevelopmentStartSummonAtHealth(division, gid, 90)
}

// Health is a fixture stimulus; the production selector still resolves the
// authored band and the production cast alone creates children.
func (rt *Runtime) DevelopmentStartSummonAtHealth(division string, gid uint32, percent uint32) (uint32, error) {
	if percent == 0 || percent > 90 {
		return 0, fmt.Errorf("fixture health must be 1..90")
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	instance, ok := rt.Monsters.Get(division, gid)
	if !ok {
		return 0, fmt.Errorf("leader absent")
	}
	remaining := uint32(uint64(instance.EffectiveMaxHP()) * uint64(percent) / 100)
	if remaining > 0 {
		remaining--
	}
	if remaining == 0 || remaining >= instance.CurrentHP {
		return 0, fmt.Errorf("invalid fixture health transition")
	}
	hit, ok := rt.Monsters.ApplyDamage(division, gid, instance.CurrentHP-remaining)
	if !ok {
		return 0, fmt.Errorf("damage seed refused")
	}
	plan, ok := rt.monsterSummonPlan(hit.Instance, 0)
	if !ok {
		return 0, fmt.Errorf("authored summon admission refused")
	}
	row, _ := rt.deps.SkillData().SkillByID(plan.SkillID)
	result := rt.monsterSummon(division, hit.Instance, row, rt.Now().UnixMilli())
	if !result.Accepted {
		return 0, fmt.Errorf("summon action refused")
	}
	if rt.PushDivisionPeerFrames != nil {
		frames := make([]wire.Frame, len(result.Frames))
		for i, frame := range result.Frames {
			frames[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload, Current: frame.Current, Scope: frame.Scope}
		}
		rt.PushDivisionPeerFrames(division, "", frames)
	}
	return plan.SkillID, nil
}

/*
==================
DevelopmentObserverProtection

Temporary fixture setup, explicitly NOT evidence for a retail skill producer.
The ordinary body-status owner and wire publisher carry the change. Cleanup
never overwrites a different status installed after this fixture's value.
==================
*/
func (rt *Runtime) DevelopmentObserverProtection(division, name string, enable bool) bool {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil {
		return false
	}
	value, expected := uint8(0), uint8(3)
	if enable {
		value, expected = 3, 0
	}
	changed := rt.deps.UpdateDeferred(c, "development-follow-observer", func() bool {
		if c.NativeBodyStatus != expected || c.DeletePending {
			return false
		}
		return c.TransitionBodyStatus(domain.BodyStatusTransition{Value: value})
	})
	if changed {
		rt.publishBodyStatus(division, name, []wire.Frame{bodyStatusFrame(enterworld.ObjectIDForCharacter(c), value)})
	}
	return changed
}

/*
==================
DevelopmentRestoreViewer

DevelopmentRestoreViewer returns a fixture viewer to the living gauge
through the retail owners. A corpse uses HandleLocalRebirth choice 2
(0x32DC present point). A living viewer is raised to the keeper ceiling
and published on 0x33A6, the same composer recovery uses. Create still
refuses a dead viewer; this does not change that check.
==================
*/
func (rt *Runtime) DevelopmentRestoreViewer(division string, character *enterworld.Character) OpResult {
	if character == nil {
		return OpResult{DiagnosticRefusal: "restore-viewer-missing"}
	}
	if !enterworld.CharacterAlive(character) {
		result := rt.HandleLocalRebirth(division, character, []byte{wire.RebirthAtPresentPoint})
		if result.DiagnosticRefusal != "" {
			return result
		}
		if !enterworld.CharacterAlive(character) {
			return OpResult{DiagnosticRefusal: "restore-viewer-still-dead"}
		}
		return result
	}
	var frames []wire.Frame
	ok := rt.deps.UpdateDeferred(character, "development-restore-viewer", func() bool {
		if !enterworld.CharacterAlive(character) || character.DeletePending {
			return false
		}
		maxHP, maxMP, currentHP, currentMP := rt.playerKeeperVitals(division, character)
		if currentHP >= maxHP && currentMP >= maxMP {
			return true
		}
		character.CurrentHP = &maxHP
		character.CurrentMP = &maxMP
		frames = []wire.Frame{{
			Opcode: simulation.OpVitalsUpdate,
			Payload: simulation.VitalsRefreshWithSourcePayload(
				enterworld.ObjectIDForCharacter(character),
				simulation.VitalsSourceNaturalRecovery,
				simulation.Vitals{CurrentHP: uint32(maxHP), CurrentMP: uint32(maxMP)},
			),
		}}
		return true
	})
	if !ok {
		return OpResult{DiagnosticRefusal: "restore-viewer-commit-refused"}
	}
	return OpResult{Frames: frames}
}
