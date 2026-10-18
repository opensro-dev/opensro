package action

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
	"reflect"
	"testing"
)

func TestShopCatalogPublishesAuthoredTabIdentity(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.NpcRoster[0].NpcTalkStoreGroups[0].Tabs[0].LabelSymbol = "SN_TAB_WEAPON"
	var projection shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &projection); err != nil {
		t.Fatal(err)
	}
	if len(projection.Tabs) != 1 || projection.Tabs[0].Index != 0 || projection.Tabs[0].LabelSymbol != "SN_TAB_WEAPON" {
		t.Fatalf("lost authored tab: %+v", projection)
	}
	if len(projection.Offers) != 1 || projection.Offers[0].Tab != projection.Tabs[0].Index {
		t.Fatalf("tab identity differs from transaction: %+v", projection)
	}
}

func merchantFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	c := testCharacter()
	items := testItems()
	ref := items["ITEM_ETC_HP_POTION_01"]
	ref.NativeFields = ref.NativeFields.With("sellPrice", 15)
	ref.NativeFields = ref.NativeFields.With("canSell", 1)
	rt, _ := newTestRuntime(c, items)
	spawn := simulation.SeedWorldState(c).Spawn
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	// An in-range shop request opened the merchant (npcrange.go).
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 2, Ref: ref, Price: 60, Stack: 50}}}}
	return rt, c
}
func trade(t *testing.T, rt *Runtime, c *enterworld.Character, q wire.ItemMoveRequest) OpResult {
	t.Helper()
	p, e := q.Encode()
	if e != nil {
		t.Fatal(e)
	}
	return rt.HandleItemMove(testDivision, c, p)
}
func TestShopBuySellAtomic(t *testing.T) {
	rt, c := merchantFixture(t)
	q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 70}
	r := trade(t, rt, c, q)
	if len(r.Frames) != 4 || r.Frames[0].Opcode != 14 || r.Frames[1].Opcode != 12 || goldOf(c) != 800 || len(c.MissionInventory) != 3 {
		t.Fatalf("purchase: %+v gold %d rows %+v", r, goldOf(c), c.MissionInventory)
	}
	q = wire.ItemMoveRequest{MovementType: 9, NpcGID: 17, SourceSlot: 13, Quantity: 20}
	r = trade(t, rt, c, q)
	if len(r.Frames) != 3 || r.Frames[2].Opcode != opShopBuyback || goldOf(c) != 1100 || c.MissionInventory[1].StackCount != 30 {
		t.Fatalf("sale: %+v %+v", r, c.MissionInventory)
	}
}
func TestShopRefusalsNeverMutate(t *testing.T) {
	for _, kind := range []string{"funds", "slot", "merchant", "distance", "equipment", "quantity", "full"} {
		t.Run(kind, func(t *testing.T) {
			rt, c := merchantFixture(t)
			q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1}
			switch kind {
			case "funds":
				q.Quantity = 100
			case "slot":
				q.ShopSlot = 3
			case "merchant":
				q.NpcGID = 18
			case "distance":
				rt.NpcRoster[0].Spawn.X += 1000
			case "equipment":
				q.MovementType = 9
				q.SourceSlot = 20
			case "quantity":
				q.MovementType = 9
				q.SourceSlot = 99
			case "full":
				for slot := int64(13); slot < 109; slot++ {
					if slot == 20 {
						continue
					}
					c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1})
				}
			}
			before := c.Snapshot()
			r := trade(t, rt, c, q)
			if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(c.MissionInventory, before.MissionInventory) || goldOf(c) != goldOf(before) {
				t.Fatalf("refusal mutated: %+v", r)
			}
		})
	}
}

func TestPackagePurchaseLimitPreservesAuthoredData(t *testing.T) {
	rt, c := merchantFixture(t)
	read := func() uint16 {
		var p shopProjection
		if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &p); err != nil {
			t.Fatal(err)
		}
		return p.Offers[0].PurchaseLimit
	}
	if got := read(); got != 50 {
		t.Fatalf("ordinary stack: %d", got)
	}
	o := rt.Commerce.Tabs[1][0]
	rt.Commerce.Tabs[1][0].Contents = []commerce.Content{{Ref: o.Ref, Stack: o.Stack, Data: 1}}
	if got := read(); got != 5 {
		t.Fatalf("preset bundle Data=1: %d", got)
	}
	rt.Commerce.Tabs[1][0].Contents = append(rt.Commerce.Tabs[1][0].Contents, commerce.Content{Ref: o.Ref, Stack: o.Stack})
	if got := read(); got != 5 {
		t.Fatalf("multi-item package: %d", got)
	}
}

/*
================
TestShopPurchaseRefusalsNameTheirCause

A refused purchase answers the native code the client turns into a notice:
0x0F without the gold, 0xD4 for an honor package (no Training Camp honor).
================
*/
func TestShopPurchaseRefusalsNameTheirCause(t *testing.T) {
	rt, c := merchantFixture(t)
	setGold(c, 0)
	rows := len(c.MissionInventory)
	q := wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 2, Quantity: 1}
	r := trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughGold)) {
		t.Fatalf("gold refusal: %+v", r)
	}
	offer := rt.Commerce.Tabs[1][0]
	offer.Currency = commerce.PaymentHonor
	rt.Commerce.Tabs[1] = []commerce.Offer{offer}
	r = trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughHonor)) {
		t.Fatalf("honor refusal: %+v", r)
	}
	if goldOf(c) != 0 || len(c.MissionInventory) != rows {
		t.Fatalf("a refused purchase changed the character: gold %d rows %d", goldOf(c), len(c.MissionInventory))
	}
}

/*
================
TestShopSellsAPetSummoner

The stable's Pet tab sells a pet summoner (3/2/1) with no record: the
bought scroll lands as an empty summoner (state 1) and summons its pet.
================
*/
func TestShopSellsAPetSummoner(t *testing.T) {
	c, refs := persistentSummonFixture()
	c.MissionInventory = c.MissionInventory[:0]
	rt, _ := newTestRuntime(c, refs)
	spawn := simulation.SeedWorldState(c).Spawn
	rt.NpcSpawn.Enabled = true
	rt.NpcRoster = []simulation.NpcDef{{ObjectID: 17, RefObjID: 100, TalkFlags: simulation.NpcTalkFlagShop, AuthoredSpawn: true, Spawn: spawn, NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1}}}}}}
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	scroll := refs.staticItemSource["SUMMON_ATTACK"]
	rt.Commerce = &commerce.Catalog{Tabs: map[int32][]commerce.Offer{1: {{Slot: 0, Ref: scroll, Price: 100, Stack: 1}}}}
	rt.BindPetSession(testDivision, c, 101)
	before := goldOf(c)
	r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: 8, NpcGID: 17, ShopSlot: 0, Quantity: 1})
	if len(r.Frames) == 0 || goldOf(c) != before-100 || len(c.MissionInventory) != 1 || c.MissionInventory[0].RefObjID != scroll.RefObjID {
		t.Fatalf("pet summoner purchase: %+v gold %d rows %+v", r.Frames, goldOf(c), c.MissionInventory)
	}
	useSummonerFixture(t, rt, c, uint8(c.MissionInventory[0].Slot), scroll)
	if len(c.Companions()) != 1 || !c.Companions()[0].Summoned {
		t.Fatal("the bought scroll did not summon its pet")
	}
}

/*
================
TestGuildStoreTwoCurrencyShopPurchase

Guild shop goods priced in Gold + Guild Points (payment 1 + payment 8)
publish both price rows in the shop catalog, require the buyer to be the
guild master (0xC4), require sufficient gold (0x0F) and guild points (0xC5),
and atomically debit both while broadcasting 0x3E74 subOp 5 (&0x08) to the
buyer and online guild members.
================
*/
func TestGuildStoreTwoCurrencyShopPurchase(t *testing.T) {
	d := guildManagerFixture(t)
	rt, c := d.rt, d.character
	guilds := d.authority.Guilds()
	setCharGold := func(ch *enterworld.Character, amount uint64) {
		if !d.authority.UpdateCharacters([]*enterworld.Character{ch}, "fixture-gold", func() bool {
			setGold(ch, amount)
			return true
		}) {
			t.Fatal("fixture gold update refused")
		}
	}

	items := testItems()
	ref := items["ITEM_ETC_HP_POTION_01"]
	rt.NpcRoster = append(rt.NpcRoster, simulation.NpcDef{
		ObjectID:      17,
		RefObjID:      100,
		TalkFlags:     simulation.NpcTalkFlagShop,
		AuthoredSpawn: true,
		Spawn:         simulation.SeedWorldState(c).Spawn,
		NpcTalkStoreGroups: []simulation.NpcTalkStoreGroup{
			{StoreGroupID: 100, Tabs: []simulation.NpcTalkStoreTab{{TabID: 1, LabelSymbol: "SN_TAB_SPECIAL"}}},
		},
	})
	rt.Selected.Set(testDivision, c.Name, 17)
	rt.Selected.OpenFunction(testDivision, c.Name, 17)
	rt.Commerce = &commerce.Catalog{
		Tabs: map[int32][]commerce.Offer{
			1: {{
				Slot:            0,
				Ref:             ref,
				Price:           500,
				Currency:        commerce.PaymentGold,
				GuildPointPrice: 1500,
				Stack:           50,
			}},
		},
	}

	var projection shopProjection
	if err := json.Unmarshal(rt.shopCatalog(testDivision, c, 17).Payload, &projection); err != nil {
		t.Fatal(err)
	}
	wantPrices := []shopPriceRow{
		{Currency: commerce.PaymentGold, Amount: "500"},
		{Currency: commerce.PaymentGuildPoint, Amount: "1500"},
	}
	if len(projection.Offers) != 1 || !reflect.DeepEqual(projection.Offers[0].Prices, wantPrices) {
		t.Fatalf("catalog prices = %+v, want %+v", projection.Offers, wantPrices)
	}

	q := wire.ItemMoveRequest{MovementType: wire.MoveTypeShopBuy, NpcGID: 17, ShopTab: 0, ShopSlot: 0, Quantity: 2}

	// Add a second guild member to test non-master refusal and member GP broadcast.
	guildID, ok := guilds.GuildOfCharacter(testDivision, c.ID)
	if !ok {
		t.Fatal("buyer not in fixture guild")
	}
	memberChar := testCharacter()
	memberChar.Name = "GuildMate"
	if err := d.authority.CreateCharacter(testDivision, "mate-account", memberChar); err != nil {
		t.Fatal(err)
	}
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, c.ID, 0, domain.GuildMemberRecord{
		CharID: memberChar.ID, JID: 77, Name: memberChar.Name, Grade: guild.JoinerGrade,
	}); refusal.Refused() {
		t.Fatalf("AddGuildMemberAs refused: %v", refusal)
	}
	setCharGold(memberChar, 10000)
	rt.Selected.Set(testDivision, memberChar.Name, 17)
	rt.Selected.OpenFunction(testDivision, memberChar.Name, 17)

	// Non-master guild member is refused with 0xC4 (UIIT_MSG_GUILD_STORE_BUY_LIMIT).
	r := trade(t, rt, memberChar, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeGuildShopNotMaster)) {
		t.Fatalf("non-master refusal = %+v, want 0xC4", r)
	}

	// Guildless player is also refused with 0xC4.
	loner := testCharacter()
	loner.Name = "Loner"
	if err := d.authority.CreateCharacter(testDivision, "loner-account", loner); err != nil {
		t.Fatal(err)
	}
	setCharGold(loner, 10000)
	rt.Selected.Set(testDivision, loner.Name, 17)
	rt.Selected.OpenFunction(testDivision, loner.Name, 17)
	r = trade(t, rt, loner, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeGuildShopNotMaster)) {
		t.Fatalf("guildless refusal = %+v, want 0xC4", r)
	}

	// Master without enough gold is refused with 0x0F and guild GP stays 6000.
	setCharGold(c, 900)
	r = trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughGold)) {
		t.Fatalf("master gold refusal = %+v, want 0x0F", r)
	}
	if rec, _, _ := guilds.Guild(testDivision, guildID); rec.GP != 6000 {
		t.Fatalf("GP mutated on gold refusal: %d", rec.GP)
	}

	// Master without enough GP (needs 2 * 1500 = 3000, set GP = 2000) is refused with 0xC5 and gold stays 10000.
	setCharGold(c, 10000)
	if _, refusal := guilds.UpdateGuildAs(testDivision, c.ID, "test-gp", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.GP = 2000
			return g, m, true
		}); refusal.Refused() {
		t.Fatal(refusal)
	}
	r = trade(t, rt, c, q)
	if len(r.Frames) != 1 || !reflect.DeepEqual(r.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeNotEnoughGuildPoints)) {
		t.Fatalf("master GP refusal = %+v, want 0xC5", r)
	}
	if goldOf(c) != 10000 {
		t.Fatalf("gold mutated on GP refusal: %d", goldOf(c))
	}

	// Master with sufficient gold (10000 >= 1000) and GP (5000 >= 3000) succeeds:
	// gold -> 9000, GP -> 2000, and 0x3E74 GP push goes to both buyer and GuildMate.
	if _, refusal := guilds.UpdateGuildAs(testDivision, c.ID, "test-gp", domain.GuildAuthorization{},
		func(g domain.GuildRecord, m []domain.GuildMemberRecord) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
			g.GP = 5000
			return g, m, true
		}); refusal.Refused() {
		t.Fatal(refusal)
	}
	var pushedMember string
	var pushedGuildID int64
	var pushedFrames []wire.Frame
	rt.PushGuildFrames = func(_ string, name string, id int64, frames []wire.Frame) {
		pushedMember = name
		pushedGuildID = id
		pushedFrames = append(pushedFrames, frames...)
	}
	r = trade(t, rt, c, q)
	if goldOf(c) != 9000 {
		t.Fatalf("gold after purchase = %d, want 9000", goldOf(c))
	}
	rec, _, _ := guilds.Guild(testDivision, guildID)
	if rec.GP != 2000 {
		t.Fatalf("guild GP after purchase = %d, want 2000", rec.GP)
	}
	wantPush := wire.Frame{Opcode: guild.OpGuildUpdatePush, Payload: guild.EncodeGuildGp3B29(2000)}
	if pushedMember != memberChar.Name || pushedGuildID != guildID || len(pushedFrames) != 1 || !reflect.DeepEqual(pushedFrames[0], wantPush) {
		t.Fatalf("member guild push = (%q, %d, %+v), want (%q, %d, %+v)",
			pushedMember, pushedGuildID, pushedFrames, memberChar.Name, guildID, wantPush)
	}
	foundBuyerPush := false
	for _, f := range r.Frames {
		if reflect.DeepEqual(f, wantPush) {
			foundBuyerPush = true
		}
	}
	if !foundBuyerPush {
		t.Fatalf("buyer frames missing 0x3E74 GP push: %+v", r.Frames)
	}
}
