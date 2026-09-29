package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
	"time"
)

func TestShippedUniqueSummonActionsAcrossAllHealthBands(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	refs := monster.LoadMonsterRefs(dir)
	skills := enterworld.NewTextdataSkills(dir)
	rt := NewRuntime(&enterworld.Deps{Skills: skills}, nil)
	covered, waves, fallbacks := 0, 0, 0
	childrenChecked := map[uint32]bool{}
	behaviorChecked := map[uint32]bool{}
	behaviorCases := map[[3]uint32]bool{}
	expectedCases := map[[3]uint32]bool{}
	missingTactics := map[string]string{}
	for _, ref := range refs {
		if ref.MonsterType != 3 {
			continue
		}
		covered++
		// Inventory directly from authored entries, independently of sampled
		// wave execution. Cartesian products of parents and grades overcount
		// combinations which no skill actually authors.
		for _, id := range ref.DefaultSkillIDs {
			row, ok := skills.SkillByID(id)
			if !ok || !row.Summon.Present {
				continue
			}
			for _, entry := range row.Summon.Entries {
				if entry.RefObjID != 0 && entry.Minimum > 0 {
					expectedCases[[3]uint32{ref.RefObjID, entry.RefObjID, uint32(entry.Grade & 15)}] = true
				}
			}
		}
		t.Run(ref.Codename, func(t *testing.T) {
			for _, hp := range []uint32{90, 70, 50, 30, 10} {
				for _, sample := range []float64{0, .999} {
					state := simulation.NewMonsterState(monster.TemplateFromParts(refs, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: 0x62aa, X: 960, Z: 960}}}))
					state.SetTimeSource(func() time.Time { return time.UnixMilli(1000) })
					state.SetRandomSource(func() float64 { return 0 })
					state.StartDivision("summon")
					state.AdvancePopulation(state.CurrentTimeMillis())
					instance := state.InstancesInRegions("summon", []uint16{0x62aa})[0]
					hit, _ := state.ApplyDamage("summon", instance.Gid, uint32((uint64(instance.EffectiveMaxHP())*uint64(100-hp)+99)/100))
					plan, ok := rt.MonsterAttackPlan(hit.Instance, 0, sample)
					var authored []monster.SummonSkill
					for _, id := range ref.DefaultSkillIDs {
						r, exists := skills.SkillByID(id)
						if exists && r.Summon.Present {
							authored = append(authored, r.Summon)
						}
					}
					if _, assigned := monster.SelectSummon(hit.Instance, authored, sample); !assigned {
						if !ok || plan.Summon {
							t.Fatalf("missing authored band must retain ordinary attack: %+v", plan)
						}
						fallbacks++
						continue
					}
					if !ok || !plan.Summon {
						t.Fatalf("HP=%d draw=%v: no summon plan: %+v", hp, sample, plan)
					}
					row, _ := skills.SkillByID(plan.SkillID)
					rt.Monsters = state
					// Exercise normal retaliation -> mover action selection -> summon
					// admission, rather than calling the summon producer directly.
					var result simulation.MonsterAttackResult
					ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return sample }, AttackPlan: rt.MonsterAttackPlan}
					ops.BasicAttack = func(division string, current monster.Instance, target, skillID uint32, now int64) simulation.MonsterAttackResult {
						selected, found := skills.SkillByID(skillID)
						if !found || !selected.Summon.Present {
							t.Fatalf("normal tick selected non-summon %d", skillID)
						}
						result = rt.monsterSummon(division, current, selected, now)
						return result
					}
					if !state.ArmRetaliation("summon", instance.Gid, simulation.PlayerObjectID(1)) {
						t.Fatal("retaliation refused")
					}
					viewer := simulation.SessionSnapshot{SessionID: "viewer", DivisionID: "summon", CharacterID: 1, CombatEligible: true,
						World: simulation.WorldState{Spawn: simulation.Spawn{RegionID: 0x62aa, X: 960, Z: 960}, SpawnSet: true}, BodyRadius: 4}
					// The observer must belong to the same admitted population as the
					// actor; the tick deliberately excludes unowned/stale sessions.
					lease, owned := state.ObjectPopulation("summon", instance.Gid)
					if !owned {
						t.Fatal("missing actor population")
					}
					viewer.Population = lease
					viewer.WorldInstance = uint32(lease.ID)
					ops.RunMonsterLeg(1000, []simulation.SessionSnapshot{viewer}, &summonTickPusher{})
					if !result.Accepted {
						t.Fatalf("HP=%d skill=%d: executable summon refused", hp, plan.SkillID)
					}
					state.AdvanceSummons(1000 + int64(row.ActionCastingTimeMs))
					want := 0
					for _, entry := range row.Summon.Entries {
						if entry.RefObjID != 0 {
							want += int(min(entry.Minimum, 50))
							if _, known := monster.SummonedSightRange(refs[entry.RefObjID], entry.Grade, func() float64 { return 0 }); !known {
								missingTactics[refs[entry.RefObjID].Codename] = refs[entry.RefObjID].OriginalCodename
							}
						}
					}
					if got := len(state.MaterializedInstances("summon")) - 1; got != want {
						t.Fatalf("skill=%d children=%d want=%d", plan.SkillID, got, want)
					}
					for _, child := range state.MaterializedInstances("summon") {
						if child.Gid == instance.Gid {
							continue
						}
						childrenChecked[child.Ref.RefObjID] = true
						mover, exists := state.Mover("summon", child.Gid)
						if !exists || mover.ControllerGID() != instance.Gid || child.SummonerGID != instance.Gid || mover.TargetGID() != 0 || child.CurrentHP != child.EffectiveMaxHP() {
							t.Fatalf("child admission/binding: %+v", child)
						}
						tactics, known := monster.ResolveSummonTactics(child.Ref, child.Rarity(), func() float64 { return 0 })
						if known {
							behaviorKey := [3]uint32{ref.RefObjID, child.Ref.RefObjID, uint32(child.Rarity())}
							if !behaviorCases[behaviorKey] {
								t.Run("child_"+child.Ref.Codename, func(t *testing.T) {
									testShippedSummonedChildDecisions(t, rt, refs, ref, child)
								})
								behaviorChecked[child.Ref.RefObjID] = true
								behaviorCases[behaviorKey] = true
							}
							if child.SummonSightRange != tactics.SightRange || child.Nest.NativeTacticsFlags != tactics.NativeFlags || child.Nest.TargetPolicy != tactics.TargetPolicy {
								t.Fatalf("child lost default tactics: %s grade=%d", child.Ref.Codename, child.Rarity())
							}
							attack, ok := rt.MonsterAttackPlan(child, 0, 0)
							if !ok || attack.Summon || attack.SkillID == 0 {
								t.Fatalf("known child has no ordinary attack plan: %s %+v", child.Ref.Codename, attack)
							}
						} else if child.SummonSightRange != 0 || child.Nest.NativeTacticsFlags != 0 || child.Nest.TargetPolicy != 0 {
							t.Fatalf("unknown tactics gained invented values: %s", child.Ref.Codename)
						}
						if !state.Defeat("summon", child.Gid, time.UnixMilli(100000)) {
							t.Fatal("child cleanup refused")
						}
					}
					state.AdvancePopulation(200000)
					if len(state.MaterializedInstances("summon")) != 1 {
						t.Fatal("summoned children independently respawned")
					}
					waves++
				}
			}
		})
	}
	if covered != 19 {
		t.Fatalf("unique coverage=%d", covered)
	}
	if waves != 182 || fallbacks != 8 || len(missingTactics) != 0 {
		t.Fatalf("coverage drift: waves=%d fallbacks=%d unknown=%d", waves, fallbacks, len(missingTactics))
	}
	t.Logf("post-spawn binding/tactics/attack-plan/cleanup coverage: %d child references", len(childrenChecked))
	if len(behaviorChecked) != 40 {
		t.Fatalf("known child decision coverage = %d", len(behaviorChecked))
	}
	t.Logf("executed %d known parent/child/grade decision cases", len(behaviorCases))
	for key := range expectedCases {
		if behaviorCases[key] {
			continue
		}
		// Not selected by the two boundary samples above. Exercise this actual
		// authored tuple through the same production child factory/lifecycle.
		t.Run("additional_authored_"+refs[key[1]].Codename, func(t *testing.T) {
			testShippedSummonedChildDecisions(t, rt, refs, refs[key[0]], monster.Instance{Ref: refs[key[1]], Nest: monster.NestRow{HasRarityOverride: true, RarityOverride: uint8(key[2])}})
		})
		behaviorCases[key] = true
	}
	if len(behaviorCases) != len(expectedCases) {
		t.Fatalf("authored child combination coverage = %d, inventory = %d", len(behaviorCases), len(expectedCases))
	}
	t.Logf("complete authored tuple lifecycle inventory: %d parent/child/grade combinations", len(behaviorCases))
	t.Logf("executed %d authored waves across %d unique rows; missing later-version tactics families: %v", waves, covered, missingTactics)
}

func TestDevelopmentSummonSelectionUsesRequestedAuthoredFamily(t *testing.T) {
	licensed.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	refs := monster.LoadMonsterRefs(dir)
	rt := NewRuntime(&enterworld.Deps{Skills: enterworld.NewTextdataSkills(dir)}, nil)
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(refs, nil))
	for _, code := range []string{"MOB_AM_IVY", "MOB_EU_KERBEROS"} {
		got, err := rt.DevelopmentSummonReferenceByCodename(code)
		if err != nil || got.Codename != code {
			t.Fatalf("requested authored family %s: %+v %v", code, got, err)
		}
	}
	if _, err := rt.DevelopmentSummonReferenceByCodename("MOB_NOT_AN_AUTHORED_UNIQUE"); err == nil {
		t.Fatal("unknown family silently chose a different unique")
	}
}

func TestMonsterSummonCastHasNoDamageAndClosesOnce(t *testing.T) {
	rt, clock, character, _ := newCombatTestRuntime(t, 1000)
	skills := rt.deps.SkillData().(staticSkillSource)
	row := skills[2]
	row.Summon = monster.SummonSkill{Present: true, HPPercent: 80, Entries: [9]monster.SummonEntry{{RefObjID: 2, Minimum: 2, Maximum: 2}}}
	row.ActionCastingTimeMs = 100
	row.ActionDurationMs = 900
	row.ActionRangePinned = true
	row.TimingPinned = true
	row.CoolTimeMs = 2000
	skills[2] = row
	parent := monster.MonsterRef{RefObjID: 1, Codename: "MOB_CH_TIGERWOMAN", MaxHP: 1000}
	parent.DefaultSkillIDs[0] = 2
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{1: parent, 2: {RefObjID: 2, MaxHP: 100}}, []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: 0x62a8}}}))
	rt.Monsters.SetTimeSource(clock.Now)
	rt.Monsters.StartDivision(testDivision)
	rt.Monsters.AdvancePopulation(rt.Monsters.CurrentTimeMillis())
	instance := rt.Monsters.InstancesInRegions(testDivision, []uint16{0x62a8})[0]
	hit, _ := rt.Monsters.ApplyDamage(testDivision, instance.Gid, 100)
	before := enterworld.CurrentHP(character)
	result := rt.MonsterBasicAttack(testDivision, hit.Instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs())
	if !result.Accepted || len(result.Frames) != 1 {
		t.Fatalf("cast=%+v", result)
	}
	frame := result.Frames[0]
	if frame.Opcode != 0xb245 || len(frame.Payload) != 19 || frame.Payload[18] != 0 {
		t.Fatalf("untargeted cast=%x", frame.Payload)
	}
	if enterworld.CurrentHP(character) != before {
		t.Fatal("summon inflicted invented damage")
	}
	if len(rt.Monsters.MaterializedInstances(testDivision)) != 1 {
		t.Fatal("wave appeared before casting boundary")
	}
	token := binary.LittleEndian.Uint32(frame.Payload[10:14])
	if result := rt.MonsterBasicAttack(testDivision, hit.Instance, enterworld.ObjectIDForCharacter(character), 2, clock.NowMs()); result.Accepted {
		t.Fatal("duplicate wave")
	}
	if frames := rt.MonsterActionTickHook()(clock.At(99 * time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatal("early release")
	}
	assertOnlySkillReleases(t, rt.MonsterActionTickHook()(clock.At(100*time.Millisecond).UnixMilli()))
	if len(rt.Monsters.MaterializedInstances(testDivision)) != 3 {
		t.Fatal("wave absent at casting boundary")
	}
	assertSkillCastClose(t, rt.MonsterActionTickHook()(clock.At(1000*time.Millisecond).UnixMilli()), testDivision, token)
	if frames := rt.MonsterActionTickHook()(clock.At(1001 * time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatal("duplicate close")
	}
}

// Packet transport is outside this test; production visibility/order has its
// own simulation regression. No synthetic action plan is injected here.
type summonTickPusher struct{}

func (*summonTickPusher) PushToSession(string, []simulation.Frame)          {}
func (*summonTickPusher) PushToDivision(string, []simulation.Frame, string) {}
