/*
===========================================================================

resultless_status_test.go - result-less lanes publish the status they land (#3680)

A hawk strike, a pulse-area tick, a returned hit and a Temptation fight
publish no cast result of their own, so the abnormal publications their
committed hit carries reach observers only through the shared impact
publisher (monsterabnormal.go): the flags-4 status frame, never the
monster's HP, which still reaches only its holders once per tick. These
tests pin that for each lane, and that a status-free hit adds no frame.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

// bleedingBit is the abnormal mask word's bleeding bit (flying_stone_test.go).
const bleedingBit = 0x800

/*
================
statusSkillParams

The shipped bleeding block of the sword smash chain's first stage: a
status that lands on the combat fixture's monster with the roll pinned
at zero (flying_stone_test.go).
================
*/
func statusSkillParams(t *testing.T) abnormal.SkillParams {
	t.Helper()
	dir := os.Getenv("SRO_SKILL_INVENTORY_DATA")
	if dir == "" {
		dir = gamedatatest.TextdataDir(t)
	}
	if _, err := os.Stat(filepath.Join(dir, "skilldata.txt")); err != nil {
		t.Skip("v1.150 textdata unavailable")
	}
	source := enterworld.NewTextdataSkills(dir)
	root, ok := source.SkillByCodename("SKILL_CH_SWORD_SMASH_D_01")
	if !ok {
		t.Fatal("SKILL_CH_SWORD_SMASH_D_01 missing")
	}
	stages, ok := enterworld.OffensiveSequence(source, root.ID)
	if !ok || len(stages) == 0 {
		t.Fatal("the sword smash chain is not admitted")
	}
	if _, ok := stages[0].Abnormal.Param(abnormal.Bleeding); !ok {
		t.Fatal("the first stage carries no bleeding")
	}
	return stages[0].Abnormal
}

/*
================
seedBleedingProc

The shared probability accumulator (combat.CriticalOutcome) stores a
threshold per caster and status key, and a lane that pre-plans its
victims (the pulse) spends the cheap first draw before its commit
rolls again. Seeding the bleeding key high keeps the authored chance
decisive no matter which planning pass rolls first; the lane under
test, not the accumulator, is what these tests measure.
================
*/
func seedBleedingProc(rt *Runtime, c *enterworld.Character) {
	actor := criticalActor{division: testDivision, character: strings.ToLower(c.Name)}
	rt.criticals.mu.Lock()
	defer rt.criticals.mu.Unlock()
	if rt.criticals.actors == nil {
		rt.criticals.actors = make(map[criticalActor]map[uint32]combat.Probability)
	}
	if rt.criticals.actors[actor] == nil {
		rt.criticals.actors[actor] = make(map[uint32]combat.Probability)
	}
	rt.criticals.actors[actor][0x0c000000] = combat.Probability{Initialized: true, Threshold: 1000}
}

/*
================
countStatusFrames

countStatusFrames counts the flags-4 status frames naming gid: the
public mask publication (MonsterAbnormalPayload), never an HP body.
================
*/
func countStatusFrames(frames []wire.Frame, gid uint32) int {
	count := 0
	for _, frame := range frames {
		p := frame.Payload
		if frame.Opcode == simulation.OpVitalsUpdate && len(p) > 6 && binary.LittleEndian.Uint32(p) == gid && p[6] == 4 {
			count++
		}
	}
	return count
}

/*
================
assertLaneStatus

A lane's public frames carry exactly want flags-4 status frames for
the monster and no frame carrying its HP.
================
*/
func assertLaneStatus(t *testing.T, label string, frames []wire.Frame, gid uint32, want int) {
	t.Helper()
	if got := countStatusFrames(frames, gid); got != want {
		t.Fatalf("%s: %d flags-4 status frames for monster %#x in %04X, want %d", label, got, gid, opcodesOf(frames), want)
	}
	assertNoMonsterHP(t, label, frames, gid)
}

/*
================
publicWireFrames

The public (division-wide) batches as wire frames; per-character
batches are not what observers see.
================
*/
func publicWireFrames(batches []simulation.DivisionFrames) []wire.Frame {
	var out []wire.Frame
	for _, batch := range batches {
		if batch.OnlyCharacterID != 0 {
			continue
		}
		for _, f := range batch.Frames {
			out = append(out, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
		}
	}
	return out
}

/*
================
simulationWireFrames
================
*/
func simulationWireFrames(frames []simulation.Frame) []wire.Frame {
	out := make([]wire.Frame, 0, len(frames))
	for _, f := range frames {
		out = append(out, wire.Frame{Opcode: f.Opcode, Payload: f.Payload})
	}
	return out
}

/*
================
tickPublicFrames

Ticks rt for ms in 100 ms steps from its clock and returns the public
frames every tick published (hawkStrikes keeps only the strikes).
================
*/
func tickPublicFrames(rt *Runtime, ms int64) []wire.Frame {
	var out []wire.Frame
	now := rt.Now()
	for step := int64(0); step < ms; step += 100 {
		now = now.Add(100 * time.Millisecond)
		at := now
		rt.Now = func() time.Time { return at }
		out = append(out, publicWireFrames(rt.TickHook()(at.UnixMilli()))...)
	}
	return out
}

/*
================
TestHawkStrikePublishesTheStatusItLands

The strike frame carries damage alone, so a status the strike commits
reaches observers only through the shared impact publisher: one
flags-4 frame beside the strike, and no frame at all when the strike
lands no status (#3680).
================
*/
func TestHawkStrikePublishesTheStatusItLands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status bool
	}{
		{"the strike lands bleeding", true},
		{"a plain strike", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, c, target, _, _ := arrowFixture(t)
			skill := shippedOffense(t, "SKILL_CH_BOW_CALL_B_01")
			// The level-1 fixture's MP cannot pay the summon; cost is not tested here.
			skill.Consumption.MP, skill.Consumption.MPPercent = 0, 0
			if tc.status {
				skill.Abnormal = statusSkillParams(t)
			}
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
			c.CurrentMP = testInt64(10000)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			if result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode()); result.DiagnosticRefusal != "" {
				t.Fatalf("summon refused: %s", result.DiagnosticRefusal)
			}
			rt.noteHawkOwnerAttack(testDivision, c, target, rt.Now().UnixMilli())
			frames := tickPublicFrames(rt, 3000)
			strikes := 0
			for _, f := range frames {
				if f.Opcode == wire.OpSummonedHawkStrike {
					strikes++
				}
			}
			if strikes != 1 {
				t.Fatalf("%d hawk strikes, want 1", strikes)
			}
			after, _ := rt.Monsters.Get(testDivision, target)
			if tc.status {
				if after.AbnormalMask()&bleedingBit == 0 {
					t.Fatalf("the strike landed no bleeding: mask %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "hawk strike", frames, target, 1)
			} else {
				if after.AbnormalMask() != 0 {
					t.Fatalf("a plain strike changed the mask: %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "plain hawk strike", frames, target, 0)
			}
		})
	}
}

/*
================
TestPulseAreaPublishesTheStatusItLands

The pulse frame is the lane's only result, so a status the pulse
commits reaches observers only through the shared impact publisher
(#3680).
================
*/
func TestPulseAreaPublishesTheStatusItLands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status bool
	}{
		{"the pulse lands bleeding", true},
		{"a plain pulse", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, _, c, row, mob := warlockFixture(t, "SKILL_EU_WARLOCK_SOULA_CHAOS_A_01")
			if tc.status {
				row.Abnormal = statusSkillParams(t)
				rt.deps.SkillData().(staticSkillSource)[row.ID] = row
				// The area pre-plan rolls the status before the commit
				// does; keep the accumulator's draw decisive (seedBleedingProc).
				seedBleedingProc(rt, c)
			}
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			now := rt.Now().UnixMilli()
			if !rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: row.ID,
				SkillGroup: row.Group, InstanceToken: 9, State: statuseffect.StateActive,
				StartedAtMs: now - 2000, DurationPresent: true, ExpiresAtMs: now + 14000}) {
				t.Fatal("Soul Chaos did not install")
			}
			rt.trackPulseArea(testDivision, c.Name)
			before, _ := rt.Monsters.Get(testDivision, mob.Gid)
			frames := publicWireFrames(rt.advancePulseAreas(now))
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if after.CurrentHP >= before.CurrentHP {
				t.Fatalf("the pulse struck nothing: HP %d -> %d", before.CurrentHP, after.CurrentHP)
			}
			pulsed := false
			for _, f := range frames {
				pulsed = pulsed || f.Opcode == wire.OpSkillPulse
			}
			if !pulsed {
				t.Fatalf("no pulse frame in %04X", opcodesOf(frames))
			}
			if tc.status {
				if after.AbnormalMask()&bleedingBit == 0 {
					t.Fatalf("the pulse landed no bleeding: mask %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "pulse area", frames, mob.Gid, 1)
			} else {
				if after.AbnormalMask() != 0 {
					t.Fatalf("a plain pulse changed the mask: %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "plain pulse area", frames, mob.Gid, 0)
			}
		})
	}
}

/*
================
TestDamageReturnPublishesTheStatusItLands

The returned hit publishes its pulse frame alone, so a status the
return commits reaches observers only through the shared impact
publisher (#3680).
================
*/
func TestDamageReturnPublishesTheStatusItLands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status bool
	}{
		{"the return lands bleeding", true},
		{"a plain return", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, mob := newCombatTestRuntime(t, 5000)
			mob.Ref.DefaultSkillIDs[0] = 2
			c.CurrentHP = testInt64(100000)
			skills := rt.deps.SkillData().(staticSkillSource)
			strike := skills[2]
			strike.Attack.Min, strike.Attack.Max, strike.Attack.Percent = 100, 100, 100
			skills[2] = strike
			row := shippedOffense(t, "SKILL_EU_WARRIOR_TWOHANDP_RETURN_A_01")
			row.Reqi = enterworld.SkillReqi{}
			row.PassiveParameters.DamageReturn = enterworld.SkillDamageReturn{Present: true, Chance: 100, Physical: 50, Range: 1000}
			if tc.status {
				row.Abnormal = statusSkillParams(t)
			}
			skills[row.ID] = row
			c.Skills = append(c.Skills, row.ID)
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			before, _ := rt.Monsters.Get(testDivision, mob.Gid)
			result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs())
			if !result.Accepted {
				t.Fatal("monster attack refused")
			}
			after, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if after.CurrentHP >= before.CurrentHP {
				t.Fatalf("the return did not land: HP %d -> %d", before.CurrentHP, after.CurrentHP)
			}
			frames := simulationWireFrames(result.Frames)
			pulsed := false
			for _, f := range frames {
				pulsed = pulsed || f.Opcode == wire.OpSkillPulse
			}
			if !pulsed {
				t.Fatalf("no pulse frame in %04X", opcodesOf(frames))
			}
			if tc.status {
				if after.AbnormalMask()&bleedingBit == 0 {
					t.Fatalf("the return landed no bleeding: mask %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "damage return", frames, mob.Gid, 1)
			} else {
				if after.AbnormalMask() != 0 {
					t.Fatalf("a plain return changed the mask: %#x", after.AbnormalMask())
				}
				assertLaneStatus(t, "plain damage return", frames, mob.Gid, 0)
			}
		})
	}
}

/*
================
TestTemptationFightPublishesNoStatusFrame

A monster fight rolls no abnormal status (temptation.go), so the
shared publisher has nothing to send for the struck monster: the
viewer receives the B245 bracket alone, and the struck monster's HP
reaches only its holders (#3680).
================
*/
func TestTemptationFightPublishesNoStatusFrame(t *testing.T) {
	f := newTemptationFight(t, 0)
	f.castTemptation(t)
	pushed := len(f.push.frames)
	bHP := f.hp(f.b.Gid)
	if !f.run(func() bool { return f.hp(f.b.Gid) < bHP }) {
		t.Fatal("the tempted monster never struck its neighbour")
	}
	struck := false
	for _, frame := range f.push.frames[pushed:] {
		if frame.Opcode != wire.OpSkillCastResult || len(frame.Payload) < skillCastTargetOffset+4 {
			continue
		}
		caster := binary.LittleEndian.Uint32(frame.Payload[skillCastCasterOffset:])
		target := binary.LittleEndian.Uint32(frame.Payload[skillCastTargetOffset:])
		struck = struck || caster == f.a.Gid && target == f.b.Gid
	}
	if !struck {
		t.Fatal("the viewer never received the tempted monster's attack on its neighbour")
	}
	if after, _ := f.rt.Monsters.Get(testDivision, f.b.Gid); after.AbnormalMask() != 0 {
		t.Fatalf("the fight changed the struck monster's mask: %#x", after.AbnormalMask())
	}
	assertLaneStatus(t, "temptation fight", simulationWireFrames(f.push.frames[pushed:]), f.b.Gid, 0)
}
