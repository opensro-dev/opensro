package action

import (
	"encoding/binary"
	"math"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
	"time"
)

func TestBerserkActivationExpiryAndDeath(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	c.BerserkPoints = 5
	rt.effects.Apply(statuseffect.Effect{DivisionID: testDivision, CharacterName: c.Name, SkillID: 1, SkillGroup: 1, InstanceToken: 1, State: statuseffect.StateActive, Movement: true, MovementPercent: 20})
	rt.refreshMovementEffects(testDivision, c, clock.NowMs())
	r := rt.HandleBerserk(testDivision, c, []byte{1})
	if c.BerserkPoints != 0 || c.NativeBodyStatus != 1 || c.BerserkUntilMs != clock.NowMs()+60000 {
		t.Fatalf("activation %+v", c)
	}
	if len(r.Frames) != 3 || r.Frames[0].Opcode != 0x30b3 || r.Frames[1].Opcode != 0x3122 || r.Frames[2].Opcode != 0x376f {
		t.Fatalf("frames %+v", r.Frames)
	}
	if speed := math.Float32frombits(binary.LittleEndian.Uint32(r.Frames[2].Payload[8:])); math.Abs(float64(speed-120)) > .00002 {
		t.Fatalf("haste plus Berserk %v", speed)
	}
	if c.ModifyBerserkPoints(1) {
		t.Fatal("gained while active")
	}
	if r := rt.HandleBerserk(testDivision, c, []byte{1}); len(r.Frames) != 1 || r.Frames[0].Payload[1] != 3 {
		t.Fatal("duplicate activation")
	}
	clock.Advance(59999 * time.Millisecond)
	if got := rt.advanceBerserk(clock.NowMs()); len(got) != 0 {
		t.Fatal("early expiry")
	}
	clock.Advance(time.Millisecond)
	got := rt.advanceBerserk(clock.NowMs())
	if c.NativeBodyStatus != 0 || c.BerserkUntilMs != 0 || len(got) != 1 {
		t.Fatal("expiry", got)
	}
	if len(rt.advanceBerserk(clock.NowMs())) != 0 {
		t.Fatal("duplicate expiry")
	}
	c.BerserkPoints = 5
	rt.HandleBerserk(testDivision, c, []byte{1})
	rt.retireBodyEffectsOnDeath(testDivision, c)
	if c.NativeBodyStatus != 0 || c.BerserkUntilMs != 0 {
		t.Fatal("death leaked Berserk")
	}
	clock.Advance(time.Minute)
	if len(rt.advanceBerserk(clock.NowMs())) != 0 {
		t.Fatal("stale death timer")
	}
}
func TestBerserkAdmissionMatrix(t *testing.T) {
	for mode := uint8(0); mode < 8; mode++ {
		for points := uint8(0); points <= 5; points++ {
			rt, _, c, _ := newCombatTestRuntime(t, 100)
			c.NativeBodyStatus = mode
			c.BerserkPoints = points
			r := rt.HandleBerserk(testDivision, c, []byte{1})
			allowed := (mode == 0 || mode == 5) && points == 5
			if allowed {
				if c.NativeBodyStatus != 1 || c.BerserkPoints != 0 {
					t.Fatalf("mode%d points%d rejected", mode, points)
				}
			} else {
				if c.NativeBodyStatus != mode || c.BerserkPoints != points || len(r.Frames) != 1 || r.Frames[0].Opcode != 0xb341 {
					t.Fatalf("mode%d points%d mutated", mode, points)
				}
			}
		}
	}
}
func TestBerserkNativeRewardThreshold(t *testing.T) {
	for _, x := range []struct {
		pc, mob int64
		rarity  uint8
		roll    uint32
		want    int
	}{{1, 1, 0, 799, 1}, {1, 1, 0, 800, 0}, {1, 1, 0, 10000, 1}, {100, 1, 0, 24, 1}, {100, 1, 0, 25, 0}, {1, 1, 3, 0, 5}, {1, 1, 4, 3999, 1}, {1, 1, 0x14, 3999, 1}, {1, 1, 4, 4000, 0}, {1, 1, 6, 1599, 1}} {
		if got := berserkKillAward(x.pc, x.mob, x.rarity, x.roll); got != x.want {
			t.Fatalf("%+v got%d", x, got)
		}
	}
}

func TestBerserkPotionConsumesOnlyAcceptedUse(t *testing.T) {
	c := testCharacter()
	items := testItems()
	ref := &enterworld.ItemRef{RefObjID: 23274, Codename: "ITEM_ETC_GNGWC_WHAN", TypeIDs: [4]int64{3, 3, 1, 8}, Country: 3, RequiredSex: 2, NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1})}
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 3}}
	rt, _ := newTestRuntime(c, items)
	p := []byte{21, 0, 0}
	binary.LittleEndian.PutUint16(p[1:], ref.TypeFlags())
	r := rt.HandleItemUse(testDivision, c, p)
	if c.BerserkPoints != 5 || c.MissionInventory[0].StackCount != 2 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("potion %+v points%d", r, c.BerserkPoints)
	}
	found := false
	for _, f := range r.Frames {
		if f.Opcode == 0x30b3 {
			found = true
			if binary.LittleEndian.Uint32(f.Payload[2:]) != 0 {
				t.Fatal("item source")
			}
		}
	}
	if !found {
		t.Fatal("missing gauge")
	}
	r = rt.HandleItemUse(testDivision, c, p)
	if c.MissionInventory[0].StackCount != 1 || r.Frames[0].Opcode != wire.OpItemUseResponse {
		t.Fatal("full gauge use")
	}
	c.NativeBodyStatus = 1
	r = rt.HandleItemUse(testDivision, c, p)
	if c.MissionInventory[0].StackCount != 1 || r.Frames[0].Payload[0] != 2 {
		t.Fatal("active consumed item")
	}
}

/*
================
TestMallHwanFillsTheBerserkGauge

The mall Hwan (9259) shares TID 3/3/13/13 with the stat recall scrolls;
only its Param1 (1, against their 2) sends it to the gauge. Using it
must not recall a stat point.
================
*/
func TestMallHwanFillsTheBerserkGauge(t *testing.T) {
	c := testCharacter()
	items := testItems()
	// The shipped row (itemdata_10000.txt 9259): TID 3/3/13/13, Param1 1.
	ref := &enterworld.ItemRef{RefObjID: 9259, Codename: "ITEM_MALL_HWAN", TypeIDs: [4]int64{3, 3, 13, 13},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 1})}
	items[ref.Codename] = ref
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1}}
	rt, _ := newTestRuntime(c, items)
	recalled := false
	rt.RecallStatPoints = func(*enterworld.Character) ([]wire.Frame, bool) { recalled = true; return nil, true }
	p := []byte{21, 0, 0}
	binary.LittleEndian.PutUint16(p[1:], ref.TypeFlags())
	r := rt.HandleItemUse(testDivision, c, p)
	if recalled {
		t.Fatal("the Hwan recalled stat points instead of filling the gauge")
	}
	if c.BerserkPoints != 5 || len(c.MissionInventory) != 0 || r.Frames[0].Payload[0] != 1 {
		t.Fatalf("hwan use %+v points %d bag %+v", r, c.BerserkPoints, c.MissionInventory)
	}
	// The family's other Param1 keeps its own lane.
	recall := &enterworld.ItemRef{RefObjID: 9253, Codename: "ITEM_MALL_STR_RECALL", TypeIDs: [4]int64{3, 3, 13, 13},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 2})}
	if family := admittedItemUseFamily(recall); family != itemUseStatRecall {
		t.Fatalf("recall family %d", family)
	}
	if family := admittedItemUseFamily(ref); family != itemUseBerserk {
		t.Fatalf("hwan family %d", family)
	}
}

func TestBerserkKillOwnerPartyAndSourceIdentity(t *testing.T) {
	rt, _, actor, mob := newCombatTestRuntime(t, 100)
	rt.BerserkRoll = func() (uint32, error) { return 0, nil }
	gid := enterworld.ObjectIDForCharacter(actor)
	peer := *actor
	peer.ID = 44
	peer.Name = "peer"
	peer.BerserkPoints = 4
	pgid := enterworld.ObjectIDForCharacter(&peer)
	party := &RewardParty{Options: 1, Members: []uint32{gid, pgid}}
	roster := rewardRoster{actors: map[uint32]rewardActor{gid: {character: actor, world: 1, party: party}, pgid: {character: &peer, world: 1, party: party}}}
	mob.Gid = 0x12345678
	mob.Nest.HasRarityOverride = true
	mob.Nest.RarityOverride = 0x14
	result := simulation.MonsterDamageResult{Instance: mob, Fatal: true}
	out := rt.grantBerserkForKill(actor, roster, result)
	if len(out) != 2 || actor.BerserkPoints != 1 || peer.BerserkPoints != 5 {
		t.Fatalf("party %+v points%d/%d", out, actor.BerserkPoints, peer.BerserkPoints)
	}
	for _, r := range out {
		if binary.LittleEndian.Uint32(r.Frames[0].Payload[2:]) != mob.Gid {
			t.Fatal("source GID truncated")
		}
	}
	actor.BerserkPoints = 0
	peer.BerserkPoints = 0
	mob.Nest.RarityOverride = 0x24
	result.Instance = mob
	out = rt.grantBerserkForKill(actor, roster, result)
	if len(out) != 1 || peer.BerserkPoints != 0 {
		t.Fatal("nonparty high nibble shared")
	}
	actor.BerserkPoints = 0
	mob.Nest.RarityOverride = 0x14
	result.Instance = mob
	r := roster.actors[pgid]
	r.world = 2
	roster.actors[pgid] = r
	out = rt.grantBerserkForKill(actor, roster, result)
	if len(out) != 1 || peer.BerserkPoints != 0 {
		t.Fatal("cross-world award")
	}
	if len(rt.grantBerserkForKill(nil, roster, result)) != 0 {
		t.Fatal("uncredited death")
	}
}

func TestBerserkDisconnectClearsRuntimeWithoutRestoringSpentPoints(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 100)
	c.BerserkPoints = 5
	rt.HandleBerserk(testDivision, c, []byte{1})
	rt.ForgetCharacter(testDivision, c.Name)
	if c.NativeBodyStatus != 0 || c.BerserkUntilMs != 0 || c.BerserkPoints != 0 {
		t.Fatal("disconnect retained body or refunded points")
	}
	clock.Advance(time.Minute)
	if len(rt.advanceBerserk(clock.NowMs())) != 0 {
		t.Fatal("disconnect retained expiry job")
	}
	c.BerserkPoints = 4
	rt.ForgetCharacter(testDivision, c.Name)
	if c.BerserkPoints != 4 {
		t.Fatal("disconnect lost unspent points")
	}
}

func TestBerserkRejectsTeleportAndMalformedRequestsWithoutMutation(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	c.BerserkPoints = 5
	for _, p := range [][]byte{nil, {}, {0}, {2}, {1, 0}} {
		if len(rt.HandleBerserk(testDivision, c, p).Frames) != 0 || c.BerserkPoints != 5 || c.NativeBodyStatus != 0 {
			t.Fatal("malformed request mutated actor")
		}
	}
	c.NativeTeleportMode = 1
	r := rt.HandleBerserk(testDivision, c, []byte{1})
	if len(r.Frames) != 1 || r.Frames[0].Opcode != 0xb341 || c.BerserkPoints != 5 || c.NativeBodyStatus != 0 {
		t.Fatal("teleport activation admitted")
	}
}
