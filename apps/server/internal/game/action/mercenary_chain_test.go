/*
===========================================================================

mercenary_chain_test.go - guild soldiers through the real guild doors

The #220 connected-verification chain: create a level-1 guild, donate the
55,800 SP the two level-ups cost, climb at the guild manager, then summon
at each of levels 3, 4 and 5. No fixture writes the guild level directly;
every rung goes through DonateGuildPoints and HandleGuildLevelUp.

===========================================================================
*/
package action

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
guildSoldierChainFixture

A store-backed guild master beside the guild manager, holding the soldier
scrolls and the exact SP/gold the level 1 -> 5 climb costs
(55,800 + 135,000 + 378,000 SP; 3M + 9M + 15M + 21M gold).
================
*/
func guildSoldierChainFixture(t *testing.T) (*doorRuntime, *enterworld.ItemRef) {
	t.Helper()
	d := openDoorRuntime(t, filepath.Join(t.TempDir(), "authority"), testCharacter())
	deps := d.rt.deps.(*enterworld.Deps)
	deps.Guilds = d.authority.Guilds()
	c := d.character
	sp := int64(55800 + 135000 + 378000)
	gold := int64(48000000)
	if !d.authority.UpdateCharacters([]*enterworld.Character{c}, "chain-wallet", func() bool {
		c.SkillPoints = &sp
		c.Gold = &gold
		return true
	}) {
		t.Fatal("chain wallet refused")
	}
	if _, err := deps.Guilds.CreateGuild(testDivision, enterworld.GuildRecord{Name: "ChainGuard", Level: 1, GP: 0},
		enterworld.GuildMemberRecord{CharID: c.ID, JID: 1, Name: c.Name, Grade: 0, PermMask: 0xffffffff, FortressRole: guild.FortressRoleCommander}, c); err != nil {
		t.Fatal(err)
	}
	npc := simulation.NpcDef{ObjectID: guildManagerGid, RefObjID: 7600, Codename: "NPC_EU_GUILD",
		AuthoredSpawn: true, Spawn: simulation.SeedWorldState(c).Spawn}
	npc.Services = simulation.ResolveNpcServices(npc)
	d.rt.NpcRoster = []simulation.NpcDef{npc}
	d.rt.NpcSpawn.Enabled = true
	d.rt.Selected.Set(testDivision, c.Name, guildManagerGid)

	refs := testCosSource(testItems())
	item := &enterworld.ItemRef{Codename: "GUILD_SCROLL", RefObjID: 9900, TypeIDs: [4]int64{3, 3, 12, 1}, Country: 3,
		AssociatedCharacterCodename: "SOLDIER_ONE", NativeFields: enterworld.NewNativeFields(map[string]float64{"canUse": 1, "itemParam1_29c": 20})}
	refs.staticItemSource[item.Codename] = item
	for i, name := range []string{"SOLDIER_ONE", "SOLDIER_TWO"} {
		ref := &enterworld.CharacterRef{Codename: name, GroupCodename: "SOLDIERS", RefObjID: uint32(9910 + i), TidWord: 0x1c6 | 5<<11,
			Level: uint8(i + 1), MaxHP: 100, MaxMP: 50, RunSpeed: 60, WalkSpeed: 20, Scale: 100}
		ref.Parameters.Country = uint8(enterworld.NativeCountryByte9C(c))
		refs.characters[name] = ref
	}
	deps.Items = refs
	d.authority.UpdateCharacter(c, "chain-scrolls", func() bool {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 23, RefObjID: item.RefObjID, Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 4})
		return true
	})
	d.rt.BindPetSession(testDivision, c, 1)
	return d, item
}

/*
================
chainGuild
================
*/
func chainGuild(t *testing.T, d *doorRuntime) domain.GuildRecord {
	t.Helper()
	guildID, ok := d.authority.Guilds().GuildOfCharacter(testDivision, d.character.ID)
	if !ok {
		t.Fatal("chain character has no guild")
	}
	record, _, ok := d.authority.Guilds().Guild(testDivision, guildID)
	if !ok {
		t.Fatal("chain guild row missing")
	}
	return record
}

/*
================
chainDonate
================
*/
func chainDonate(t *testing.T, d *doorRuntime, amount uint32) {
	t.Helper()
	result, refusal := d.authority.Guilds().DonateGuildPoints(testDivision, d.character.ID, amount)
	if refusal.Refused() {
		t.Fatalf("donate %d refused: %v", amount, refusal)
	}
	if result.Donor.DonatedGP == 0 {
		t.Fatalf("donate %d left no donor credit", amount)
	}
}

/*
================
chainLevelUp
================
*/
func chainLevelUp(t *testing.T, d *doorRuntime, wantLevel uint8, wantGP uint32) {
	t.Helper()
	request := wire.NewWriter(4).U32(guildManagerGid).Payload()
	out := d.rt.HandleGuildLevelUp(testDivision, d.character, request)
	assertOpcodes(t, out.Frames, opGuildLevelUpResponse, guild.OpGuildUpdatePush, wire.OpPointsUpdate)
	record := chainGuild(t, d)
	if record.Level != wantLevel || record.GP != wantGP {
		t.Fatalf("after level up: level %d GP %d, want %d/%d", record.Level, record.GP, wantLevel, wantGP)
	}
}

/*
================
chainScrollsLeft
================
*/
func chainScrollsLeft(t *testing.T, d *doorRuntime) int64 {
	t.Helper()
	for _, row := range d.character.MissionInventory {
		if row.Slot == 23 {
			return row.StackCount
		}
	}
	return 0
}

/*
================
chainSummon

Summons at the current clock and pins the step-1/3 facts: the soldier
count the guild level allows, unique owned GIDs, the 1,200 s expiry and
cooldown stamps, and exactly one scroll consumed.
================
*/
func chainSummon(t *testing.T, d *doorRuntime, item *enterworld.ItemRef, wantCount int, scrollsBefore int64) {
	t.Helper()
	c := d.character
	nowSec := d.rt.Now().Unix()
	useSummonerFixture(t, d.rt, c, 23, item)
	if len(c.Mercenaries) != wantCount {
		t.Fatalf("summoned %d soldiers, want %d at guild level %d", len(c.Mercenaries), wantCount, chainGuild(t, d).Level)
	}
	seen := map[uint32]bool{}
	for _, pet := range c.Mercenaries {
		if seen[pet.GID] || !c.OwnsMercenaryID(pet.GID) {
			t.Fatalf("soldier identity: %+v", pet)
		}
		if pet.RentalExpiresAtUnix != nowSec+1200 {
			t.Fatalf("soldier expiry %d, want %d", pet.RentalExpiresAtUnix, nowSec+1200)
		}
		seen[pet.GID] = true
	}
	if want := (nowSec + domain.MercenarySummonSeconds) * 1000; c.MercenarySummonUntilMs != want {
		t.Fatalf("cooldown stamp %d, want %d", c.MercenarySummonUntilMs, want)
	}
	if left := chainScrollsLeft(t, d); left != scrollsBefore-1 {
		t.Fatalf("scrolls left %d, want %d", left, scrollsBefore-1)
	}
}

/*
================
chainDismissKeepsCooldown

Step 8 per rung: the group leaves, the 1,200 s summon job does not.
================
*/
func chainDismissKeepsCooldown(t *testing.T, d *doorRuntime, item *enterworld.ItemRef) {
	t.Helper()
	c := d.character
	until := c.MercenarySummonUntilMs
	out := d.rt.HandleMercenaryDismiss(testDivision, c, nil)
	if len(c.Mercenaries) != 0 || c.MercenarySummonUntilMs != until {
		t.Fatalf("dismiss: %d soldiers, cooldown %d (was %d)", len(c.Mercenaries), c.MercenarySummonUntilMs, until)
	}
	if len(out.Frames) == 0 {
		t.Fatal("dismiss published nothing")
	}
	request := wire.NewWriter(3).U8(23).U16(item.TypeFlags()).Payload()
	if got := d.rt.HandleItemUse(testDivision, c, request); !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryCooldownActive).Frames[0].Payload) {
		t.Fatalf("re-summon during cooldown answered %+v", got)
	}
}

/*
================
TestGuildSoldierLifecycleChain

Create -> donate 55,800 SP -> level 3 -> summon (1) -> duplicate refused
-> dismiss keeps the 1,200 s job -> cooldown expires -> donate + level 4
-> summon (3) -> level 5 -> summon (6). Wallets end empty to the point.
================
*/
func TestGuildSoldierLifecycleChain(t *testing.T) {
	d, item := guildSoldierChainFixture(t)
	c := d.character

	chainDonate(t, d, 55800)
	if got := chainGuild(t, d).GP; got != 55800 {
		t.Fatalf("guild GP %d after donation, want 55800", got)
	}
	chainLevelUp(t, d, 2, 50400)
	chainLevelUp(t, d, 3, 0)

	// Step 1 + 3 at level 3: one soldier, one scroll.
	chainSummon(t, d, item, 1, 4)

	// Step 2: a second scroll while the group stands is refused.
	request := wire.NewWriter(3).U8(23).U16(item.TypeFlags()).Payload()
	if got := d.rt.HandleItemUse(testDivision, c, request); !bytes.Equal(got.Frames[0].Payload, itemUseFailure(mercenaryAlreadySummoned).Frames[0].Payload) {
		t.Fatalf("duplicate summon answered %+v", got)
	}
	if left := chainScrollsLeft(t, d); left != 3 {
		t.Fatalf("refused summon consumed a scroll: %d left", left)
	}

	// Step 8, then the cooldown gate itself.
	chainDismissKeepsCooldown(t, d, item)
	d.clock.Advance(1200 * time.Second)
	d.rt.advanceMercenaryCooldowns(d.clock.NowMs())

	// Level 4: donate the rung, climb, summon three.
	chainDonate(t, d, 135000)
	chainLevelUp(t, d, 4, 0)
	chainSummon(t, d, item, 3, 3)
	chainDismissKeepsCooldown(t, d, item)
	d.clock.Advance(1200 * time.Second)
	d.rt.advanceMercenaryCooldowns(d.clock.NowMs())

	// Level 5: donate the rung, climb, summon six.
	chainDonate(t, d, 378000)
	chainLevelUp(t, d, 5, 0)
	chainSummon(t, d, item, 6, 2)
	chainDismissKeepsCooldown(t, d, item)

	if *c.SkillPoints != 0 || goldOf(c) != 0 {
		t.Fatalf("wallets after the climb: SP %d, gold %d", *c.SkillPoints, goldOf(c))
	}
}
