/*
===========================================================================

playerstrike_cos_test.go - a player's hits on a COS, judged through its owner

===========================================================================
*/
package action

import (
	"bytes"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/pk"
	"opensro.online/server/internal/game/social/guildwar"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
assertRefusalCode
================
*/
func assertRefusalCode(t *testing.T, frames []wire.Frame, code uint16) {
	t.Helper()
	want := offensiveRefusal(code).Frames[0]
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && bytes.Equal(frame.Payload, want.Payload) {
			return
		}
	}
	t.Fatalf("frames %v did not include refusal %#x", frames, code)
}

/*
================
equipStrikeTestCOS

equipCombatTestPet over the runtime's own items: the combat fixture's
weapons stay, so the attacker keeps a valid loadout.
================
*/
func equipStrikeTestCOS(t *testing.T, rt *Runtime, character *enterworld.Character) *enterworld.CharacterRef {
	t.Helper()
	deps := rt.deps.(*enterworld.Deps)
	base, ok := deps.Items.(staticItemSource)
	if !ok {
		t.Fatalf("fixture items are %T, want staticItemSource", deps.Items)
	}
	ref := equipCombatTestPet(t, rt, character, cosBandTransport)
	deps.Items = cosTestItemSource{staticItemSource: base, characters: map[string]*enterworld.CharacterRef{ref.Codename: ref}}
	return ref
}

/*
================
TestPlayerBasicAttackStrikesAnotherPlayersCOS

A player's basic attack on another player's summoned COS lands on the COS's
own body: the COS loses HP, its owner does not, and the owner hears its
companion's private frames.
================
*/
func TestPlayerBasicAttackStrikesAnotherPlayersCOS(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	ref := equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: cos.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if cos.CurrentHP >= ref.MaxHP {
		t.Fatalf("COS HP %d, want below %d", cos.CurrentHP, ref.MaxHP)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("owner HP %d, want the untouched 100: the COS takes the hit", *owner.CurrentHP)
	}
}

/*
================
TestCOSTargetIsJudgedThroughItsOwner

The reference server's attack checks on the owner: an attacker in body mode 2, a target COS in
body mode 2..4, or an owner in body mode 2..4 refuses with 0x3020 and the
COS keeps its HP; hidden modes 6..7 do not resolve as a target; a player's
own COS is never a target.
================
*/
func TestCOSTargetIsJudgedThroughItsOwner(t *testing.T) {
	for _, body := range []uint8{0, 1, 2, 3, 4, 5, 6, 7} {
		t.Run(fmt.Sprintf("cos body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			owner.ActiveCOS.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if body >= mercenaryBodyHiddenFirst && body <= mercenaryBodyHiddenLast {
				if ok {
					t.Fatalf("hidden COS body=%d resolved as a target", body)
				}
				return
			}
			if !ok || target.cos == nil {
				t.Fatalf("COS body=%d must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body >= mercenaryBodyProtectedFirst && body <= mercenaryBodyProtectedLast {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(cos body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("protected COS body=%d HP %d, want untouched %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			} else if owner.ActiveCOS.CurrentHP >= ref.MaxHP {
				t.Fatalf("unprotected COS body=%d HP %d, want below %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
			}
		})
	}
	for _, body := range []uint8{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("owner body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			owner.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if !ok || target.cos == nil {
				t.Fatalf("owner body=%d: the COS must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body >= mercenaryBodyProtectedFirst && body <= mercenaryBodyProtectedLast {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(owner body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("protected owner body=%d COS HP %d, want untouched %d", body, owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			}
		})
	}
	for _, body := range []uint8{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("attacker body=%d", body), func(t *testing.T) {
			rt, _, attacker, owner := newPvpPair(t)
			ref := equipStrikeTestCOS(t, rt, owner)
			attacker.NativeBodyStatus = body
			target, ok := rt.resolveCombatTarget(testDivision, attacker, owner.ActiveCOS.GID, 0)
			if !ok || target.cos == nil {
				t.Fatalf("attacker body=%d: the COS must resolve as a COS target", body)
			}
			wantCode := uint16(0)
			if body == untouchableBodyStatus {
				wantCode = 0x3020
			}
			if code := rt.cosTargetRefusal(testDivision, attacker, target, 0); code != wantCode {
				t.Fatalf("cosTargetRefusal(attacker body=%d) = %#x, want %#x", body, code, wantCode)
			}
			out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: owner.ActiveCOS.GID}.Encode())
			if wantCode != 0 {
				if owner.ActiveCOS.CurrentHP != ref.MaxHP {
					t.Fatalf("untouchable attacker COS HP %d, want untouched %d", owner.ActiveCOS.CurrentHP, ref.MaxHP)
				}
				assertRefusalCode(t, out.Frames, wantCode)
			}
		})
	}
	t.Run("own COS", func(t *testing.T) {
		rt, _, attacker, _ := newPvpPair(t)
		equipStrikeTestCOS(t, rt, attacker)
		if _, ok := rt.resolveCombatTarget(testDivision, attacker, attacker.ActiveCOS.GID, 0); ok {
			t.Fatal("a player's own COS resolved as a target")
		}
	})
}

/*
================
TestThiefCannotStrikeALevelOneCaravanTransport

The 1-star caravan protection is the owner's, so it holds for the
transport too: a dressed thief is refused against a dressed trader's
transport carrying a level-1 caravan (0x3024), and admitted once the
transport is empty.
================
*/
func TestThiefCannotStrikeALevelOneCaravanTransport(t *testing.T) {
	rt, _, trader := caravanFixture(t)
	thief := *trader
	thief.ID, thief.Name, thief.ActiveCOS = trader.ID+1, "thief", nil
	items := rt.deps.ItemReferences().(cosTestItemSource).staticItemSource
	for i, c := range []*enterworld.Character{trader, &thief} {
		suit := &enterworld.ItemRef{RefObjID: uint32(9301 + i), Codename: []string{"ITEM_CH_M_TRADE_TRADER_05", "ITEM_CH_M_TRADE_THIEF_05"}[i],
			Country: 3, TypeIDs: [4]int64{3, 1, 7, int64(1 + i)}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, Combat: &enterworld.ItemCombatRef{}}
		items[suit.Codename] = suit
		c.MissionInventory = append(append([]enterworld.InventoryRow(nil), c.MissionInventory...), enterworld.InventoryRow{
			Slot: int64(jobSuitSlot), RefObjID: suit.RefObjID, Codename: suit.Codename, TypeFlags: suit.TypeFlags(), StackCount: 1})
	}
	for _, c := range []*enterworld.Character{trader, &thief} {
		rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
			func(w *simulation.WorldState) {
				w.Spawn = simulation.Spawn{RegionID: pvpRegion, X: 960, Y: 20, Z: 458}
				w.SpawnSet = true
			})
	}
	target, ok := rt.resolveCombatTarget(testDivision, &thief, trader.ActiveCOS.GID, 0)
	if !ok || target.cos == nil || target.player != trader {
		t.Fatalf("the ridden transport must resolve as a COS target owned by the trader: %+v", target)
	}
	if code := rt.cosTargetRefusal(testDivision, &thief, target, 0); code != 0x3024 {
		t.Fatalf("thief on a level-1 caravan's transport = %#x, want 0x3024", code)
	}
	// A target is a detached snapshot: re-resolve after the cargo leaves.
	trader.ActiveCOS.Container.Rows = nil
	if target, ok = rt.resolveCombatTarget(testDivision, &thief, trader.ActiveCOS.GID, 0); !ok {
		t.Fatal("the empty transport must still resolve")
	}
	if code := rt.cosTargetRefusal(testDivision, &thief, target, 0); code != 0 {
		t.Fatalf("thief on an empty transport = %#x, want admitted", code)
	}
}

/*
================
TestPlayerKillOfATransportScattersItsCargo

A player's fatal hit on a transport settles through the companion owner's
death, as a monster's does: the goods drop around it unowned, and its
kind-1 release deletes the record. The owner is untouched. A live
transport carries only trade goods, which the publish's owner reset
skips; the scatter publishes every row unowned, so a stack of potions
stands in for them here.
================
*/
func TestPlayerKillOfATransportScattersItsCargo(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	transport := owner.ActiveCOS
	potion := testItems()["ITEM_ETC_HP_POTION_01"]
	transport.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
		Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
	transport.CurrentHP = 1
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if owner.ActiveCOS != nil {
		t.Fatal("the killed transport kept its record")
	}
	ground := rt.Ground.All(testDivision)
	if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
		t.Fatalf("cargo %+v, want the 7 potions unowned", ground)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("owner HP %d, want 100", *owner.CurrentHP)
	}
}

/*
================
TestPlayerKillOfACOSEasesTheKillersPenalty

The reference server books the kill against the owner first (a murder here:
total +1, penalty +1200, daily +1), then eases the
killer's penalty by the gap to the companion's level, and the killer
hears its 0x30F2; a blow that does not kill books and eases nothing.
================
*/
func TestPlayerKillOfACOSEasesTheKillersPenalty(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	equipStrikeTestCOS(t, rt, owner)
	transport := owner.ActiveCOS
	transport.Level = uint8(rewardLevel(attacker)) + 4
	attacker.PK = &domain.PKRecord{Penalty: 100}
	transport.CurrentHP = 1
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if attacker.PK.Penalty != 1290 {
		t.Fatalf("penalty %d after the kill, want 1290 (100 + 1200 murder booked, then 10 relieved)", attacker.PK.Penalty)
	}
	if attacker.PK.TotalCount != 1 || attacker.PK.DailyCount != 1 {
		t.Fatalf("kill book %+v, want one murder and one daily PK", attacker.PK)
	}
	if _, ok := findFrame(out.Frames, wire.OpPKPenalty); !ok {
		t.Fatal("no 0x30F2 for the killer")
	}
}

/*
================
TestPlayerKillOfACOSBooksTheKillAgainstItsOwner

The reference server's PvP kill rewards with the COS as actual victim: an open-field kill of a neutral
owner's COS is a murder against the owner (never against the COS), and a
guild-war kill of a COS scores from the COS's own level with no
murder booked.
================
*/
func TestPlayerKillOfACOSBooksTheKillAgainstItsOwner(t *testing.T) {
	t.Run("murder", func(t *testing.T) {
		rt, _, attacker, owner := newPvpPair(t)
		equipStrikeTestCOS(t, rt, owner)
		transport := owner.ActiveCOS
		transport.CurrentHP = 1
		out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
		if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
			t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
		}
		if attacker.PK == nil || attacker.PK.TotalCount != 1 || attacker.PK.DailyCount != 1 {
			t.Fatalf("killer book %+v, want one murder against the owner", attacker.PK)
		}
		if owner.PK != nil && (owner.PK.TotalCount != 0 || owner.PK.Penalty != 0) {
			t.Fatalf("owner book %+v, want untouched: the COS is not a murder victim", owner.PK)
		}
	})
	t.Run("guild war scores from the COS level", func(t *testing.T) {
		rt, _, attacker, owner := newPvpPair(t)
		var err error
		rt.GuildWars, err = guildwar.New(testDivision, warRelationStore{})
		if err != nil {
			t.Fatal(err)
		}
		attacker.GuildID, owner.GuildID = testInt64(7), testInt64(8)
		equipStrikeTestCOS(t, rt, owner)
		transport := owner.ActiveCOS
		transport.Level = uint8(rewardLevel(attacker)) + 10
		transport.CurrentHP = 1
		var credits []domain.GuildWarCombat
		rt.GuildWarKill = func(_ string, combat domain.GuildWarCombat, _ int64) { credits = append(credits, combat) }
		out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: transport.GID}.Encode())
		if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
			t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
		}
		want := domain.GuildWarCombat{WarID: 77, KillerID: attacker.ID, VictimID: owner.ID, Score: 250}
		if len(credits) != 1 || credits[0] != want {
			t.Fatalf("war credits %+v, want [%+v] (the COS's level, not the owner's)", credits, want)
		}
		if attacker.PK != nil && attacker.PK.TotalCount != 0 {
			t.Fatalf("war kill became murder %+v", attacker.PK)
		}
	})
	t.Run("job kill pays job EXP on the owner's level only", func(t *testing.T) {
		rt, _, attacker, owner := newPvpPair(t)
		var jobPaid, expPaid int64
		rt.UpdateJobExperience = func(c *enterworld.Character, delta int64) ([]wire.Frame, bool) {
			if c == attacker {
				jobPaid += delta
			}
			return nil, true
		}
		rt.UpdateExperience = func(c *enterworld.Character, exp, _ int64, _ uint32) ([]wire.Frame, bool) {
			if c == attacker {
				expPaid += exp
			}
			return nil, true
		}
		rt.deps.(*enterworld.Deps).Levels = jobKillLevels{LevelDataSource: rt.deps.LevelData(), gold: map[int64]int64{1: 80}}
		kill := playerKill{kind: pk.DeathJob}
		_, _, after, _ := rt.payCOSKillInDoor(testDivision, attacker, owner, kill, 5, 0)
		want := jobKillExperience(rt.deps.LevelData(), attacker, rewardLevel(owner))
		if want <= 0 || jobPaid != want {
			t.Fatalf("job EXP %d, want %d from the owner's level", jobPaid, want)
		}
		if expPaid != 0 {
			t.Fatalf("PvP EXP paid %d: the native death penalty never runs on a COS death", expPaid)
		}
		if len(after.shares) != 0 || len(after.notices) != 0 {
			t.Fatalf("payout %+v, want no shares or notices for a solo COS job kill", after)
		}
	})
}

/*
================
TestPlayerAttackOnAMountedPlayerStrikesTheRide

Reference server: a player's single-target attack aimed at a player riding a
transport lands on the transport; the rider keeps every HP.
================
*/
func TestPlayerAttackOnAMountedPlayerStrikesTheRide(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	ref := equipStrikeTestCOS(t, rt, owner)
	ride := owner.ActiveCOS
	ride.Mounted = true
	out := rt.HandleTargetInteract(testDivision, attacker, wire.BasicAttackEngage{TargetGid: enterworld.ObjectIDForCharacter(owner)}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("no cast result: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if ride.CurrentHP >= ref.MaxHP {
		t.Fatalf("ride HP %d, want below %d: the mounted rider's hit lands on the ride", ride.CurrentHP, ref.MaxHP)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("rider HP %d, want the untouched 100", *owner.CurrentHP)
	}
}

/*
================
TestAreaCandidatesSpareTheRiderAndTakeTheRide

The reference server excludes a mounted player and admits companions
through their owner with mode 3: the ride is a candidate, its rider is
not, and an unmounted companion is a candidate too.
================
*/
func TestAreaCandidatesSpareTheRiderAndTakeTheRide(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	ride := owner.ActiveCOS
	ride.Mounted = true
	now := clock.NowMs()
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Present: true}}
	q := areaQuery{selects: areaSelectHostile, division: testDivision, caster: attacker, skill: skill,
		center: rt.liveSpawn(simulation.WorldKey(testDivision, attacker.Name), attacker, now), reach: 50, now: now}
	var riderFound, rideFound bool
	for _, candidate := range rt.areaPlayerCandidates(q) {
		if candidate.target.player == owner && candidate.target.cos == nil {
			riderFound = true
		}
		if candidate.target.gid == ride.GID && candidate.target.cos != nil {
			rideFound = true
		}
	}
	if riderFound {
		t.Fatal("the mounted rider is an area candidate: the native hostile-target check excludes it")
	}
	if !rideFound {
		t.Fatal("the ridden transport is not an area candidate")
	}
}

/*
================
TestAreaStrikeOnACOSBooksTheKillAgainstItsOwner

An area victim that is a COS takes the area's running percent on its own
body, and a fatal area hit books the kill against the owner exactly as a
single-target COS kill does (through the owner).
================
*/
func TestAreaStrikeOnACOSBooksTheKillAgainstItsOwner(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	cos.CurrentHP = 1
	now := clock.NowMs()
	skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	attacker.Skills = append(attacker.Skills, skill.ID)
	attacker.Intellect = testInt64(200)
	attacker.CurrentMP = testInt64(1000)
	target, ok := rt.resolveCOSTarget(testDivision, attacker, cos.GID, now)
	if !ok {
		t.Fatal("the COS does not resolve as an area victim")
	}
	stats, _, err := rt.playerCombatStats(testDivision, attacker)
	if err != nil {
		t.Fatalf("attacker stats: %v", err)
	}
	plan, planned := rt.planAreaVictims(areaPlanInput{division: testDivision, caster: attacker, snapshot: attacker,
		skill: skill, attacker: stats, victims: []combatTarget{target}, impacts: 1, now: now})
	if !planned || len(plan.cosHits) != 1 {
		t.Fatalf("area plan %+v, want one COS victim", plan)
	}
	roster := rt.monsterRewardRoster(testDivision, attacker, now)
	var commit areaCommit
	var committed bool
	if !rt.deps.UpdateMany(roster.characters, "test-area-cos", func() bool {
		commit, committed = rt.commitAreaInDoor(testDivision, attacker, roster, &plan, now)
		return committed
	}) || !committed {
		t.Fatal("the area commit refused the COS victim")
	}
	if cos.CurrentHP != 0 {
		t.Fatalf("COS HP %d, want 0 after the fatal area hit", cos.CurrentHP)
	}
	if attacker.PK == nil || attacker.PK.TotalCount != 1 || attacker.PK.DailyCount != 1 {
		t.Fatalf("attacker PK %+v, want the murder booked against the COS's owner", attacker.PK)
	}
	if len(commit.cosPublic) == 0 {
		t.Fatal("the COS victim published nothing")
	}
}

/*
================
TestAreaSkillOnAMountedPlayerStrikesTheRide

The reference server swaps a mounted target for its ride at admission, so an area row
aimed at the rider resolves with the ride as primary: the ride takes the
hit, the rider keeps every HP (the Bug 3 area arm of the redirect).
================
*/
func TestAreaSkillOnAMountedPlayerStrikesTheRide(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	ref := equipStrikeTestCOS(t, rt, owner)
	ride := owner.ActiveCOS
	ride.Mounted = true
	skill := shippedOffense(t, "SKILL_CH_LIGHTNING_CHUNDUNG_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	attacker.Skills = append(attacker.Skills, skill.ID)
	attacker.Intellect = testInt64(200)
	attacker.CurrentMP = testInt64(1000)
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true,
		TargetGid: enterworld.ObjectIDForCharacter(owner)}.Encode())
	if len(out.Frames) == 0 {
		t.Fatalf("area cast on the mounted player refused (%s)", out.DiagnosticRefusal)
	}
	if ride.CurrentHP >= ref.MaxHP {
		t.Fatalf("ride HP %d, want below %d: the redirected area must strike the ride", ride.CurrentHP, ref.MaxHP)
	}
	if *owner.CurrentHP != 100 {
		t.Fatalf("rider HP %d, want the untouched 100", *owner.CurrentHP)
	}
}

/*
================
cosTestSkill

A cloned combat-fixture skill under a test id, shaped by shape.
================
*/
func cosTestSkill(t *testing.T, rt *Runtime, id uint32, shape func(*enterworld.SkillRow)) enterworld.SkillRow {
	t.Helper()
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[2]
	skill.ID, skill.Group, skill.Codename = id, 900, "TEST_COS_SKILL"
	skill.CombatPinned, skill.DirectOffensePinned = false, false
	skill.ActionCastingTimeMs = 0
	skill.Consumption = enterworld.SkillConsumption{Pinned: true, MP: 5}
	shape(&skill)
	source[skill.ID] = skill
	return skill
}

/*
================
TestPeriodicSkillPulsesOnACOSAndBooksTheKill

A periodic row installs its linked pair on a COS (never installed before
this fix) and its pulses strike the COS's own body; a fatal pulse books
the kill against the owner.
================
*/
func TestPeriodicSkillPulsesOnACOSAndBooksTheKill(t *testing.T) {
	rt, clock, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	cos.CurrentHP = 1
	skill := cosTestSkill(t, rt, 9105, func(skill *enterworld.SkillRow) {
		skill.Attack = enterworld.SkillAttack{}
		skill.ActionDurationMs = 500
		skill.EffectRider = true
		skill.TimedEffect.Periodic = enterworld.SkillPeriodicEffect{Pinned: true, DurationMs: 12000, PeriodMs: 2000,
			Link:   enterworld.SkillEffectLink{Present: true, Group: 9, MaxOutgoing: 2},
			Attack: enterworld.SkillAttack{Present: true, Flags: 8, Percent: 37, Min: 4, Max: 6, ImpactCount: 1}}
	})
	attacker.Skills = append(attacker.Skills, skill.ID)
	attacker.CurrentMP = testInt64(100)
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: cos.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpAttachedEffect); !ok {
		t.Fatalf("no linked recipient on the COS: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	rt.advancePeriodicEffects(clock.NowMs() + 2000)
	if cos.CurrentHP != 0 {
		t.Fatalf("COS HP %d, want 0 after the fatal pulse", cos.CurrentHP)
	}
	if attacker.PK == nil || attacker.PK.TotalCount != 1 {
		t.Fatalf("attacker PK %+v, want the pulse kill booked against the owner", attacker.PK)
	}
}

/*
================
TestLifeStealSkillStrikesACOS

An lfst row strikes the COS with the life-steal record instead
of refusing 0x3006, and the caster recovers from the committed damage.
================
*/
func TestLifeStealSkillStrikesACOS(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	ref := equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[2]
	skill.LifeSteal = enterworld.SkillLifeSteal{Present: true, Amount: 1000}
	source[skill.ID] = skill
	attacker.Skills = append(attacker.Skills, skill.ID)
	attacker.CurrentMP = testInt64(100)
	attacker.CurrentHP = testInt64(50)
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: cos.GID}.Encode())
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("life-steal cast on the COS refused: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if cos.CurrentHP >= ref.MaxHP {
		t.Fatalf("COS HP %d, want below %d after the life-steal hit", cos.CurrentHP, ref.MaxHP)
	}
	if *attacker.CurrentHP <= 50 {
		t.Fatalf("caster HP %d, want recovery from the drained COS", *attacker.CurrentHP)
	}
}

/*
================
TestChargeSkillReachesACOS

A charge row aimed at a COS plans its travel to the COS's body instead
of refusing 0x3006: the only refusal this fixture may produce is the
navigation diagnostic, never the unsupported-row code.
================
*/
func TestChargeSkillReachesACOS(t *testing.T) {
	rt, _, attacker, owner := newPvpPair(t)
	rt.RewardActorPresent = func(string, string) bool { return true }
	equipStrikeTestCOS(t, rt, owner)
	cos := owner.ActiveCOS
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[2]
	skill.PositionEffect = enterworld.SkillPositionEffect{Charge: true, Range: 10}
	source[skill.ID] = skill
	attacker.Skills = append(attacker.Skills, skill.ID)
	attacker.CurrentMP = testInt64(100)
	out := rt.HandleTargetInteract(testDivision, attacker, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: cos.GID}.Encode())
	if out.DiagnosticRefusal == "charge-navigation-refused" {
		// The fixture has no nav coverage for the travel plan; reaching
		// that diagnostic already proves the row left the 0x3006 gate.
		return
	}
	if _, ok := findFrame(out.Frames, wire.OpSkillCastResult); !ok {
		t.Fatalf("charge cast on the COS refused: %+v (%s)", out.Frames, out.DiagnosticRefusal)
	}
	if cos.CurrentHP == 0 {
		t.Fatal("the COS died to a non-fatal charge fixture hit")
	}
}
