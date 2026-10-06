/*
===========================================================================

lifecontrol_teleport_test.go - Life Control replacement across world loading

Assert the packets produced during replacement, not only the settled state
after retirement: two simultaneous -50 percent writes briefly publish zero.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
lifeControlFixture
================
*/
func lifeControlFixture(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, enterworld.SkillRow) {
	t.Helper()
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 11
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	deps := rt.deps.(*enterworld.Deps)
	deps.EntrySkills = rt.EntrySkills
	deps.PlayerBaseStats = func(c *enterworld.Character) (wire.BaseStats, error) {
		return rt.PlayerBaseStats(testDivision, c)
	}
	return rt, clock, c, skill
}

/*
================
TestLifeControlSurvivesTeleportWithoutStacking

Both loading paths must preserve the effect, and every replacement packet
must contain a valid gauge even before the regular retirement tick.
================
*/
func TestLifeControlSurvivesTeleportWithoutStacking(t *testing.T) {
	for _, route := range []string{"recast", "gate", "return-scroll"} {
		for _, lowHP := range []bool{false, true} {
			t.Run(route+"/"+map[bool]string{false: "full", true: "one-hp"}[lowHP], func(t *testing.T) {
				rt, clock, c, skill := lifeControlFixture(t)
				base, err := rt.PlayerBaseStats(testDivision, c)
				if err != nil {
					t.Fatal(err)
				}
				c.CurrentHP = testInt64(int64(base.MaxHP))
				if lowHP {
					c.CurrentHP = testInt64(1)
				}
				wantMax := base.MaxHP / 2
				var ended []uint32
				check := func(op uint16, payload []byte) {
					switch op {
					case wire.OpBaseStats:
						if got := binary.LittleEndian.Uint32(payload[24:]); got != wantMax {
							t.Fatalf("published maximum HP %d, want %d", got, wantMax)
						}
					case simulation.OpVitalsUpdate:
						if len(payload) >= 11 && payload[6]&1 != 0 {
							if hp := binary.LittleEndian.Uint32(payload[7:]); hp == 0 || hp > wantMax {
								t.Fatalf("published HP %d outside 1..%d", hp, wantMax)
							}
						}
					case wire.OpEndedEffectInstances:
						packet, err := wire.DecodeEndedEffectInstances(payload)
						if err != nil {
							t.Fatal(err)
						}
						ended = append(ended, packet.InstanceTokens...)
					}
				}
				rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
					for _, frame := range frames {
						check(frame.Opcode, frame.Payload)
					}
				}
				cast := func() {
					now := clock.NowMs()
					out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
					frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
					if !ok || frame.Payload[0] != 1 {
						t.Fatal("cast refused", out)
					}
					for _, f := range out.Frames {
						check(f.Opcode, f.Payload)
					}
					for _, batch := range rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1) {
						for _, f := range batch.Frames {
							check(f.Opcode, f.Payload)
						}
					}
					stats, err := rt.PlayerBaseStats(testDivision, c)
					if err != nil || stats.MaxHP != wantMax {
						t.Fatal("replacement overlap", stats, err)
					}
					for _, batch := range rt.drainStoppedCharacterEffects() {
						for _, f := range batch.Frames {
							check(f.Opcode, f.Payload)
						}
					}
					wait := max(skill.ActionCastingTimeMs+skill.ActionDurationMs, skill.CoolTimeMs) + 1
					clock.Advance(time.Duration(wait) * time.Millisecond)
					rt.drainSkillFinalizes(clock.NowMs())
				}
				cast()
				wantHP := int64(wantMax)
				if lowHP {
					wantHP = 1
				}
				if *c.CurrentHP != wantHP {
					t.Fatalf("stored HP %d after first cast, want %d", *c.CurrentHP, wantHP)
				}
				old := rt.effects.Snapshot(testDivision, c.Name)[0]
				switch route {
				case "gate":
					destination := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
					destination.X += 50
					out := rt.commitGateTravel(gateTravel{division: testDivision, character: c, destination: destination, world: instance.ID(domain.CharacterWorldInstance(c)), reason: "test-teleport"},
						func() (int64, OpResult, bool) { return 0, OpResult{}, true })
					if len(out.Frames) == 0 || out.Frames[0].Opcode != enterworld.OpcodeResetClient {
						t.Fatal("gate did not reenter", out)
					}
					for _, f := range out.Frames {
						check(f.Opcode, f.Payload)
					}
				case "return-scroll":
					_, scrollCharacter, _, ref := returnFixture(t, 5000)
					rt.deps.ItemReferences().(staticItemSource)[ref.Codename] = ref
					for _, item := range scrollCharacter.MissionInventory {
						if item.Codename == ref.Codename {
							c.MissionInventory = append(c.MissionInventory, item)
						}
					}
					out := useReturn(rt, c)
					if f, ok := findFrame(out.Frames, wire.OpItemUseResponse); !ok || f.Payload[0] != 1 {
						t.Fatal("return refused", out)
					}
					job, ok := rt.returnCasts.Load(simulation.WorldKey(testDivision, c.Name))
					if !ok {
						t.Fatal("no return timer")
					}
					clock.Advance(5 * time.Second)
					frames, _ := rt.completeReturnScroll(job.(pendingReturn), clock.NowMs())
					if len(frames) == 0 || frames[0].Opcode != enterworld.OpcodeResetClient {
						t.Fatal("scroll did not reenter", frames)
					}
					for _, f := range frames {
						check(f.Opcode, f.Payload)
					}
				}
				if effects := rt.effects.Snapshot(testDivision, c.Name); len(effects) != 1 || effects[0].InstanceToken != old.InstanceToken {
					t.Fatal("travel changed effect ownership", effects)
				}
				cast()
				if len(ended) != 1 || ended[0] != old.InstanceToken {
					t.Fatal("old token not retired exactly once", ended)
				}
				if enterworld.CurrentHP(c) == 0 {
					t.Fatal("replacement killed caster")
				}
				if lowHP && enterworld.CurrentHP(c) != 1 {
					t.Fatal("replacement healed low HP caster")
				}
				live := rt.effects.Snapshot(testDivision, c.Name)
				wantMax = base.MaxHP
				rt.effects.Expire(live[0].ExpiresAtMs + 1)
				rt.drainStoppedCharacterEffects()
				if *c.CurrentHP != wantHP {
					t.Fatalf("buff expiry restored HP: %d, want %d", *c.CurrentHP, wantHP)
				}
			})
		}
	}
}

/*
================
TestLifeControlReplacementAdmissionPrecedesPreparation

A rejected conflict cannot charge MP or open a cast. An accepted recast
requests retirement at admission even if its preparation is then canceled.
================
*/
func TestLifeControlReplacementAdmissionPrecedesPreparation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		rt, clock, c, skill := lifeControlFixture(t)
		rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		rt.advanceProjectileCasts(clock.NowMs() + int64(skill.ActionCastingTimeMs) + 1)
		clock.Advance(time.Duration(skill.CoolTimeMs+skill.ActionCastingTimeMs+skill.ActionDurationMs+1) * time.Millisecond)
		rt.drainSkillFinalizes(clock.NowMs())
		if !cancel {
			skill = shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_B_01")
			rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
			c.Skills = append(c.Skills, skill.ID)
		}
		mp := *c.CurrentMP
		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
		if !ok {
			t.Fatal("missing cast response")
		}
		if !cancel {
			if frame.Payload[0] != 2 || *c.CurrentMP != mp || rt.hasOpenSkillCast(testDivision, c.Name) {
				t.Fatal("conflicting buff entered preparation or charged MP", out)
			}
			continue
		}
		if frame.Payload[0] != 1 {
			t.Fatal("recast refused", out)
		}
		rt.cancelPreparingProjectile(testDivision, c.Name)
		rows := rt.effects.Snapshot(testDivision, c.Name)
		if len(rows) != 1 || !rows[0].StopRequested {
			t.Fatal("canceled recast lost native admission retirement", rows)
		}
		rt.drainStoppedCharacterEffects()
		if len(rt.effects.Snapshot(testDivision, c.Name)) != 0 || enterworld.CurrentHP(c) == 0 {
			t.Fatal("canceled replacement retained an effect or killed the caster")
		}
	}
}
