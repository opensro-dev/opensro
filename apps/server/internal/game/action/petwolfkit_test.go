/*
===========================================================================
petwolfkit_test.go - wolf books through manual commands and real combat doors.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"fmt"
	"slices"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestWolfHuntingHowlTargetsCooldownAndExpiry
================
*/
func TestWolfHuntingHowlTargetsCooldownAndExpiry(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, _ := wolfFieldFixture(t, 1)
			c.MissionInventory = nil
			near := nearbyCharacter(rt, c, 21, "howl-near", 50)
			far := nearbyCharacter(rt, c, 22, "howl-far", 201)
			rt.RewardParties = func(string) []RewardParty {
				return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near), enterworld.ObjectIDForCharacter(far)}}}
			}
			skill := learnPetBook(t, c, tier)
			out := rt.manualPetSkill(testDivision, c, c, c.ActiveCOS.GID, skill.ID, 0, clock.NowMs())
			state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
			step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
			if !ok || rt.petAttackSpeed(step) <= 1 || state.wolf.howlUntil != clock.NowMs()+8000 {
				t.Fatal("wolf not buffed")
			}
			if (rt.EntryActionSpeed(testDivision, c.Name) < 100) != (tier >= 2) || (rt.EntryActionSpeed(testDivision, near.Name) < 100) != (tier == 3) || rt.EntryActionSpeed(testDivision, far.Name) != 100 {
				t.Fatal("owner/party tier or range")
			}
			emotes := 0
			for _, f := range out.Broadcast {
				if f.Opcode == wire.OpSkillCastResult {
					t.Fatal("howl added a cast")
				}
				if f.Opcode == wire.OpActionEmote {
					// The emote's action is the skill ID: the client's pet-howl clip.
					if emote := (wire.ActionEmotePush{Gid: c.ActiveCOS.GID, Action: skill.ID}).Encode(); string(f.Payload) != string(emote) {
						t.Fatal("howl emote", f.Payload)
					}
					emotes++
				}
			}
			if emotes != 1 || state.wolf.howlHoldUntil != clock.NowMs()+wolfHowlHoldMs {
				t.Fatal("howl animation hold", emotes)
			}
			deadline := c.ActiveCOS.Training.Rows[0].ReadyAtMs
			if deadline != clock.NowMs()+25000 {
				t.Fatal("wrong shared cooldown", deadline-clock.NowMs())
			}
			rt.manualPetSkill(testDivision, c, c, c.ActiveCOS.GID, skill.ID, 0, clock.NowMs()+100)
			if c.ActiveCOS.Training.Rows[0].ReadyAtMs != deadline {
				t.Fatal("cooling cast spent another row")
			}
			clock.now = clock.now.Add(8 * time.Second)
			rt.advanceWolfKit(step.key, clock.NowMs())
			if rt.EntryActionSpeed(testDivision, c.Name) != 100 || rt.EntryActionSpeed(testDivision, near.Name) != 100 || len(rt.wolfHowls.rows) != 0 {
				t.Fatal("expired howl remains")
			}
		})
	}
}

/*
================
TestWolfHowlStrongestGrantAndEligibility
================
*/
func TestWolfHowlStrongestGrantAndEligibility(t *testing.T) {
	rt, clock, c, _ := wolfFieldFixture(t, 1)
	c.MissionInventory = nil
	skill := learnPetBook(t, c, 3)
	rt.castWolfHowl(testDivision, c, c.ActiveCOS.GID, skill, clock.NowMs())
	base := rt.EntryActionSpeed(testDivision, c.Name)
	other := petOwnerKey{testDivision, "other-wolf", 123}
	rt.wolfHowls.rows[other] = wolfHowl{until: clock.NowMs() + 8000, percent: 10, members: map[uint32]string{enterworld.ObjectIDForCharacter(c): c.Name}}
	if got := rt.EntryActionSpeed(testDivision, c.Name); got != base {
		t.Fatal("howls stacked", got, base)
	}
	delete(rt.wolfHowls.rows, other)
	c.ActiveCOS.Summoned = false
	key := petOwnerKey{testDivision, "asd2", c.ActiveCOS.GID}
	rt.advanceWolfKit(key, clock.NowMs())
	if rt.EntryActionSpeed(testDivision, c.Name) != 100 {
		t.Fatal("unsummoned wolf left owner buff")
	}
	c.ActiveCOS.Summoned = true
	c.World.Spawn.RegionID = testInt64(0x62a8)
	rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
	before := c.ActiveCOS.Training.Rows
	c.ActiveCOS.Training.Rows[0].ReadyAtMs = 0
	out := rt.castWolfHowl(testDivision, c, c.ActiveCOS.GID, skill, clock.NowMs())
	if len(out.Broadcast) != 0 || c.ActiveCOS.Training.Rows[0].ReadyAtMs != 0 {
		t.Fatal("town cast applied")
	}
	c.ActiveCOS.Training.Rows = before
	rt.AttackPetExpansion = false
	if len(rt.wolfHowlWrites(testDivision, c)) != 0 {
		t.Fatal("flag-off bonus")
	}
}

/*
================
TestWolfMaximumHowlAppliesSixfoldRate
================
*/
func TestWolfMaximumHowlAppliesSixfoldRate(t *testing.T) {
	rt, clock, c, _ := wolfFieldFixture(t, 1)
	c.MissionInventory = nil
	skill := learnPetBook(t, c, 3)
	c.ActiveCOS.Training.Levels[skill.Index()] = 10
	rt.castWolfHowl(testDivision, c, c.ActiveCOS.GID, skill, clock.NowMs())
	step, ok := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	if !ok || rt.petAttackSpeed(step) != 6 || rt.EntryActionSpeed(testDivision, c.Name) != float32(100.0/6) {
		t.Fatal("maximum Howl did not reach +500% for wolf and owner")
	}
	peer := rt.companionPresentation(testDivision, step.state, c.ActiveCOS)
	if peer == nil || peer.Row.Scale != float32(100.0/6) || rt.EntryCompanionActionSpeed(testDivision, c, c.ActiveCOS) != peer.Row.Scale {
		t.Fatal("peer/entry reset the wolf animation rate")
	}
}

/*
================
TestWolfHowlExpiryBroadcastsToRecipientViewers
================
*/
func TestWolfHowlExpiryBroadcastsToRecipientViewers(t *testing.T) {
	rt, clock, c, _ := wolfFieldFixture(t, 1)
	c.MissionInventory = nil
	near := nearbyCharacter(rt, c, 21, "howl-party", 50)
	ownerGID, partyGID := enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(near)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{ownerGID, partyGID}}}
	}
	skill := learnPetBook(t, c, 3)
	rt.castWolfHowl(testDivision, c, c.ActiveCOS.GID, skill, clock.NowMs())
	clock.now = clock.now.Add(8 * time.Second)
	key := petOwnerKey{division: testDivision, name: c.Name, gid: c.ActiveCOS.GID}
	rt.advancePetInDivision(key, clock.NowMs())
	seen := map[uint32]bool{}
	for _, batch := range rt.drainMonsterLegRecipients() {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpActionSpeed && len(frame.Payload) == 8 {
				gid := binary.LittleEndian.Uint32(frame.Payload)
				if gid == ownerGID || gid == partyGID {
					if batch.SourceGID != gid || string(frame.Payload) != string(wire.ActionSpeedFrame(gid, 100).Payload) {
						t.Fatal("expiry did not restore the recipient's own viewers")
					}
					seen[gid] = true
				}
			}
		}
	}
	if !seen[ownerGID] || !seen[partyGID] {
		t.Fatalf("missing public expiry: %v", seen)
	}
}

/*
================
TestWolfBloodscentStacksAreIndependentAndExpire
================
*/
func TestWolfBloodscentStacksAreIndependentAndExpire(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			skill := learnPetBook(t, c, 9+tier)
			step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
			rt.drainMonsterLegRecipients()
			for range 12 {
				rt.wolfKitAttack(step, 0, []uint32{mob.Gid, mob.Gid})
			}
			full := 0
			for _, batch := range rt.drainMonsterLegRecipients() {
				for _, f := range batch.Frames {
					if f.Opcode == wire.OpActionEmote && string(f.Payload) == string(wolfEmoteFrame(c.ActiveCOS.GID, wolfEmoteBloodFull).Payload) {
						full++
					}
				}
			}
			if full != 1 {
				t.Fatalf("full-stack signals %d, want one at the cap", full)
			}
			for range 2 {
				rt.wolfKitAttack(step, 1, []uint32{mob.Gid})
			}
			cap := wolfBloodCap(int(tier))
			if step.state.wolf.blood[0] != cap || (step.state.wolf.blood[1] == 2) != (tier >= 2) {
				t.Fatal("stack cap or owner tier", step.state.wolf.blood)
			}
			damage, crit := wolfDamageBonus(step, 0)
			if damage != float64(cap)*skill.ValueAt(1) || (crit > 0) != (tier == 3) || crit > 5 {
				t.Fatal("wrong stack bonuses", damage, crit)
			}
			writes := rt.wolfBloodWrites(testDivision, c)
			if (len(writes) > 0) != (tier >= 2) {
				t.Fatal("owner projection")
			}
			f := petSkillCounterFrame(step.state, c.ActiveCOS.GID)
			if len(f.Payload) != 17 || f.Payload[10] != cap {
				t.Fatal("stack wire snapshot", f.Payload)
			}
			step.nowMs += 6000
			if damage, crit = wolfDamageBonus(step, 0); damage != 0 || crit != 0 {
				t.Fatal("stack expiry")
			}
			rt.advanceWolfKit(step.key, step.nowMs)
			if step.state.wolf.blood != [2]uint8{} {
				t.Fatal("expired stack badge")
			}
		})
	}
}

/*
================
TestWolfSavageRendRealCleaveAndBleed
================
*/
func TestWolfSavageRendRealCleaveAndBleed(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, mob := wolfFieldFixture(t, 1)
			skill := learnPetBook(t, c, 3+tier)
			at := rt.LiveSpawnFor(testDivision, c)
			at.X += 35
			for range 6 {
				wolfSpawnAt(t, rt, c, at, clock.NowMs())
				at.X++
			}
			payload := binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID), mob.Gid)
			rt.HandleCosCommand(testDivision, c, payload)
			frames := tickPetCombat(t, rt, clock, 100, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
			emote := (wire.ActionEmotePush{Gid: c.ActiveCOS.GID, Action: skill.ID}).Encode()
			if !slices.ContainsFunc(frames, func(f simulation.Frame) bool {
				return f.Opcode == wire.OpActionEmote && string(f.Payload) == string(emote)
			}) {
				t.Fatal("the owner did not see the rend emote")
			}
			hit := 0
			for _, m := range rt.Monsters.InstancesInRegions(testDivision, []uint16{uint16(*c.World.Spawn.RegionID)}) {
				if m.CurrentHP < m.Ref.MaxHP {
					hit++
				}
			}
			want := []int{1, 3, 5}[tier-1]
			if hit != want {
				t.Fatalf("cleave hit%d want%d", hit, want)
			}
			live, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if live.Abnormal == nil || !live.Abnormal.Slots[abnormal.Bleeding].Active {
				t.Fatal("bleed missing")
			}
			if c.ActiveCOS.Training.Rows[1].ReadyAtMs-clock.NowMs() != 12000 {
				t.Fatal("rend cooldown")
			}
		})
	}
}

/*
================
TestWolfOwnerBloodscentChangesRealAttackDamage
================
*/
func TestWolfOwnerBloodscentChangesRealAttackDamage(t *testing.T) {
	damage := func(boost bool) uint32 {
		rt, clock, c, _ := wolfFieldFixture(t, 1)
		armed, _, _, _ := newCombatTestRuntime(t, 100000)
		weapon, _ := armed.deps.ItemReferences().ItemRefByCodename("ITEM_CH_SWORD_01_A")
		rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[weapon.Codename] = weapon
		if boost {
			learnPetBook(t, c, 11)
			step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
			step.state.wolf.bloodBook, step.state.wolf.blood[1], step.state.wolf.bloodUntil[1] = 11, 8, clock.NowMs()+6000
			rt.refreshWolfBlood(step, true)
		}
		at := rt.LiveSpawnFor(testDivision, c)
		at.X += 3
		mob := wolfSpawnAt(t, rt, c, at, clock.NowMs())
		rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: mob.Gid}.Encode())
		var dealt uint32
		tickPetCombat(t, rt, clock, 100, func() bool {
			live, _ := rt.Monsters.Get(testDivision, mob.Gid)
			dealt = mob.CurrentHP - live.CurrentHP
			return dealt > 0
		})
		return dealt
	}
	normal, boosted := damage(false), damage(true)
	if boosted <= normal {
		t.Fatal("owner's real attack ignored Bloodscent", normal, boosted)
	}
}

/*
================
TestWolfRendBleedingKillDoesNotChargeHunt
================
*/
func TestWolfRendBleedingKillDoesNotChargeHunt(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	rt.Monsters.SetAbnormalContext(monsterAbnormalContext{rt})
	learnPetBook(t, c, 4)
	learnPetBook(t, c, 13)
	payload := binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 4), mob.Gid)
	rt.HandleCosCommand(testDivision, c, payload)
	tickPetCombat(t, rt, clock, 100, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, 1, clock.NowMs()) })
	rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandFollowTag).Payload())
	live, _ := rt.Monsters.Get(testDivision, mob.Gid)
	wolfWeaken(t, rt, live, 1)
	step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	before, charges := step.state.wolfHits, step.state.wolf.huntHits
	clock.now = clock.now.Add(2100 * time.Millisecond)
	rt.advanceMonsterAbnormal(testDivision, mob.Gid, clock.NowMs())
	dead, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if dead.CurrentHP != 0 || step.state.wolf.huntHits != charges {
		t.Fatal("bleed kill charged Blood Hunt", dead.CurrentHP, step.state.wolf.huntHits)
	}
	if step.state.wolfHits != before {
		t.Fatal("bleed kill charged an attack")
	}
}

/*
================
TestWolfHowlAcceleratesRealAttackCadence

A wire-value check alone missed the original inverted denominator.
================
*/
func TestWolfHowlAcceleratesRealAttackCadence(t *testing.T) {
	for _, owner := range []bool{false, true} {
		t.Run(fmt.Sprint(owner), func(t *testing.T) {
			interval := func(boost bool) int64 {
				rt, clock, c, mob := wolfFieldFixture(t, 1)
				rows := rt.deps.SkillData().(staticSkillSource)
				row := rows[2]
				row.ActionKind = 2
				rows[2] = row
				if owner {
					armed, _, _, _ := newCombatTestRuntime(t, 100000)
					weapon, _ := armed.deps.ItemReferences().ItemRefByCodename("ITEM_CH_SWORD_01_A")
					rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[weapon.Codename] = weapon
				}
				if boost {
					skill := learnPetBook(t, c, 3)
					c.ActiveCOS.Training.Levels[skill.Index()] = 10
					rt.castWolfHowl(testDivision, c, c.ActiveCOS.GID, skill, clock.NowMs())
				}
				if owner {
					rt.HandleTargetInteract(testDivision, c, wire.BasicAttackEngage{TargetGid: mob.Gid}.Encode())
				} else {
					rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, mob.Gid))
				}
				hp, first, second := mob.CurrentHP, int64(0), int64(0)
				tickPetCombat(t, rt, clock, 100, func() bool {
					after, _ := rt.Monsters.Get(testDivision, mob.Gid)
					if after.CurrentHP < hp {
						if first == 0 {
							first = clock.NowMs()
						} else {
							second = clock.NowMs()
						}
						hp = after.CurrentHP
					}
					return second > first
				})
				return second - first
			}
			normal, boosted := interval(false), interval(true)
			t.Logf("owner=%v interval normal=%dms Howl=%dms", owner, normal, boosted)
			if boosted <= 0 || boosted >= normal {
				t.Fatalf("Howl slowed attacks: normal=%d boosted=%d", normal, boosted)
			}
		})
	}
}

/*
================
TestWolfBloodscentPublishesActualAttackStatsAndRestores
================
*/
func TestWolfBloodscentPublishesActualAttackStatsAndRestores(t *testing.T) {
	rt, clock, c, mob := wolfFieldFixture(t, 1)
	armed, _, _, _ := newCombatTestRuntime(t, 100000)
	weapon, _ := armed.deps.ItemReferences().ItemRefByCodename("ITEM_CH_SWORD_01_A")
	rt.deps.ItemReferences().(cosTestItemSource).staticItemSource[weapon.Codename] = weapon
	learnPetBook(t, c, 12)
	base, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	step, _ := rt.wolfCompanion(testDivision, c, c.ActiveCOS.GID, clock.NowMs())
	for range 4 {
		rt.wolfKitAttack(step, 1, []uint32{mob.Gid})
	}
	boosted, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	if boosted.PhysicalAttackMin <= base.PhysicalAttackMin || boosted.MagicalAttackMin <= base.MagicalAttackMin || boosted.PhysicalAttackMin != uint32(stats.PhysicalAttackMin) || boosted.MagicalAttackMin != uint32(stats.MagicalAttackMin) {
		t.Fatal("panel and actual attack diverged", base, boosted, stats)
	}
	published := false
	for _, batch := range rt.drainMonsterLegRecipients() {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpBaseStats {
				published = true
			}
		}
	}
	if !published {
		t.Fatal("no private character-sheet refresh")
	}
	clock.now = clock.now.Add(6 * time.Second)
	rt.advanceWolfKit(step.key, clock.NowMs())
	reset, err := rt.PlayerBaseStats(testDivision, c)
	if err != nil || reset != base {
		t.Fatal("expired attack stats", err, base, reset)
	}
}
