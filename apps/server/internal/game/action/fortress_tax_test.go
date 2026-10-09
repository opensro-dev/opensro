/*
===========================================================================

fortress_tax_test.go - manager collection through the real authority store

Seed the durable treasury before Restore, then exercise the wire dispatcher,
NPC admission, persisted guild membership and the committed balance reply.

===========================================================================
*/
package action

import (
	"bytes"
	"fmt"
	"math"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/fortress"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	fortressTaxTestNPC            = uint32(17)
	fortressTaxTestID             = uint32(1)
	fortressTaxTestGold           = int64(60000)
	fortressTaxTestTreasury       = int64(1200)
	fortressTaxTestRate           = int16(12)
	fortressTaxCollectRequestSize = 17
)

/*
================
fortressTaxFixture
================
*/
func fortressTaxFixture(t *testing.T) *doorRuntime {
	t.Helper()
	merchant, seed := merchantFixture(t)
	seed.Gold = testInt64(fortressTaxTestGold)
	d := openDoorRuntime(t, t.TempDir(), seed)
	rt, c := d.rt, d.character
	rt.NpcRoster, rt.NpcSpawn = merchant.NpcRoster, merchant.NpcSpawn
	rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressManager)
	rt.Selected.Set(testDivision, c.Name, fortressTaxTestNPC)
	rt.Guilds = d.authority.Guilds()
	guildID, err := rt.Guilds.CreateGuild(testDivision, domain.GuildRecord{Name: "TaxGuild", Level: 1},
		domain.GuildMemberRecord{CharID: c.ID, JID: uint32(c.ID), Name: c.Name, Grade: 0, Level: 1, RefObjID: 1907}, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.authority.Fortresses().SaveFortress(testDivision, domain.FortressRecord{
		FortressID: fortressTaxTestID, GuildID: guildID, TaxGold: fortressTaxTestTreasury, TaxRate: fortressTaxTestRate,
	}); err != nil {
		t.Fatal(err)
	}
	rt.Fortresses = fortress.New([]fortress.Catalog{{ID: fortressTaxTestID}})
	if err := rt.Fortresses.Restore(testDivision, d.authority.Fortresses()); err != nil {
		t.Fatal(err)
	}
	rt.Fortresses.SetPeriod(testDivision, fortress.PeriodTax, true)
	return d
}

/*
================
fortressTaxCollectPayload
================
*/
func fortressTaxCollectPayload(id uint32, amount int64) []byte {
	return wire.NewWriter(fortressTaxCollectRequestSize).U32(fortressTaxTestNPC).
		U8(siege.ActionTaxCollect).U32(id).U64(uint64(amount)).Payload()
}

/*
================
assertFortressTaxBalances

Check both the authority's live projection and the durable store row.
================
*/
func assertFortressTaxBalances(t *testing.T, d *doorRuntime, gold, treasury int64) {
	t.Helper()
	if d.character.Gold == nil || *d.character.Gold != gold {
		t.Fatalf("master gold = %v, want %d", d.character.Gold, gold)
	}
	record, exists := d.rt.Fortresses.Get(testDivision, fortressTaxTestID)
	if !exists || record.TaxGold != treasury {
		t.Fatalf("live treasury = %+v, exists %v, want %d", record, exists, treasury)
	}
	rows, _, err := d.authority.Fortresses().FortressState(testDivision)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].FortressID != fortressTaxTestID || rows[0].TaxGold != treasury || rows[0].TaxRate != fortressTaxTestRate {
		t.Fatalf("saved fortress = %+v, want treasury %d and preserved tax rate", rows, treasury)
	}
}

/*
================
TestFortressTaxCollectWireBalancesAndPersistence
================
*/
func TestFortressTaxCollectWireBalancesAndPersistence(t *testing.T) {
	d := fortressTaxFixture(t)
	gold, treasury := fortressTaxTestGold, fortressTaxTestTreasury
	// docs/fortress-tax-native.md: the recovered SQL refuses a withdrawal
	// above the treasury instead of partially paying the requested amount.
	steps := []struct {
		name                 string
		requested, collected int64
		refusal              uint8
	}{
		{"negative", -1, 0, 0},
		{"minimum signed", math.MinInt64, 0, 0},
		{"zero", 0, 0, 0},
		{"partial", 350, 350, 0},
		{"maximum signed exceeds remaining", math.MaxInt64, 0, 2},
		{"exact remaining", fortressTaxTestTreasury - 350, fortressTaxTestTreasury - 350, 0},
		{"empty treasury positive refused", 1, 0, 2},
		{"empty treasury zero succeeds", 0, 0, 0},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			out := d.rt.HandleFortressInteraction(testDivision, d.character, fortressTaxCollectPayload(fortressTaxTestID, step.requested))
			gold += step.collected
			treasury -= step.collected
			if step.refusal != 0 {
				want := []byte{siege.ActionTaxCollect, 2, step.refusal}
				if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
					t.Fatalf("overamount reply = %+v, want %x without gold refresh", out.Frames, want)
				}
			} else {
				if step.collected != 0 {
					if len(out.Frames) != 2 || out.Frames[0].Opcode != wire.OpPointsUpdate {
						t.Fatalf("gold refresh must precede B1E1: %+v", out.Frames)
					}
					refresh, err := wire.DecodeGoldRefresh(out.Frames[0].Payload)
					if err != nil || refresh.Balance != uint64(gold) || refresh.Notify {
						t.Fatalf("gold refresh = %+v, error %v, want %d without gain notification", refresh, err, gold)
					}
					out.Frames = out.Frames[1:]
				}
				want := wire.NewWriter(10).U8(siege.ActionTaxCollect).U8(1).U64(uint64(step.collected)).Payload()
				if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
					t.Fatalf("collection reply = %+v, want only B1E1 %x", out.Frames, want)
				}
			}
			assertFortressTaxBalances(t, d, gold, treasury)
			query := wire.NewWriter(9).U32(fortressTaxTestNPC).U8(siege.ActionTaxQuery).U32(fortressTaxTestID).Payload()
			out = d.rt.HandleFortressInteraction(testDivision, d.character, query)
			want := wire.NewWriter(16).U8(siege.ActionTaxQuery).U8(1).U32(fortressTaxTestID).
				U16(uint16(fortressTaxTestRate)).U64(uint64(treasury)).Payload()
			if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
				t.Fatalf("tax query = %+v, want remaining treasury %d", out.Frames, treasury)
			}
		})
	}
	d = d.reboot(t)
	d.rt.Fortresses = fortress.New([]fortress.Catalog{{ID: fortressTaxTestID}})
	if err := d.rt.Fortresses.Restore(testDivision, d.authority.Fortresses()); err != nil {
		t.Fatal(err)
	}
	assertFortressTaxBalances(t, d, gold, treasury)
}

/*
================
fortressTaxGuest

Use stored membership rather than forging the runtime's guild read model.
================
*/
func fortressTaxGuest(t *testing.T, d *doorRuntime, membership string) *enterworld.Character {
	t.Helper()
	guest := testCharacter()
	guest.Name = "TaxGuest"
	if err := d.authority.CreateCharacter(testDivision, "tax-guest", guest); err != nil {
		t.Fatal(err)
	}
	d.rt.Selected.Set(testDivision, guest.Name, fortressTaxTestNPC)
	switch membership {
	case "member":
		guildID, _ := d.rt.Guilds.GuildOfCharacter(testDivision, d.character.ID)
		_, refusal := d.rt.Guilds.AddGuildMemberAs(testDivision, guildID, d.character.ID, 0,
			domain.GuildMemberRecord{CharID: guest.ID, JID: uint32(guest.ID), Name: guest.Name, Grade: guild.JoinerGrade, Level: 1, RefObjID: 1907})
		if refusal.Refused() {
			t.Fatalf("fixture guild join: %v", refusal)
		}
	case "foreign":
		_, err := d.rt.Guilds.CreateGuild(testDivision, domain.GuildRecord{Name: "OtherTax", Level: 1},
			domain.GuildMemberRecord{CharID: guest.ID, JID: uint32(guest.ID), Name: guest.Name, Grade: 0, Level: 1, RefObjID: 1907}, guest)
		if err != nil {
			t.Fatal(err)
		}
	}
	return guest
}

/*
================
TestFortressTaxCollectRefusalOrderAndMembership
================
*/
func TestFortressTaxCollectRefusalOrderAndMembership(t *testing.T) {
	for _, tc := range []struct {
		name, membership string
		closed, unknown  bool
		code             uint8
	}{
		{"closed before unknown and guildless", "guildless", true, true, 8},
		{"closed before member rank", "member", true, false, 8},
		{"unknown before guildless", "guildless", false, true, 3},
		{"unknown for master", "master", false, true, 3},
		{"guildless", "guildless", false, false, 6},
		{"non-master member", "member", false, false, 7},
		{"foreign master", "foreign", false, false, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := fortressTaxFixture(t)
			actor := d.character
			if tc.membership != "master" {
				actor = fortressTaxGuest(t, d, tc.membership)
			}
			before := goldOf(actor)
			d.rt.Fortresses.SetPeriod(testDivision, fortress.PeriodTax, !tc.closed)
			id := fortressTaxTestID
			if tc.unknown {
				id++
			}
			out := d.rt.HandleFortressInteraction(testDivision, actor, fortressTaxCollectPayload(id, math.MaxInt64))
			want := []byte{siege.ActionTaxCollect, 2, tc.code}
			if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
				t.Fatalf("refusal = %+v, want %x", out.Frames, want)
			}
			if goldOf(actor) != before {
				t.Fatalf("refusal changed requester gold from %d to %d", before, goldOf(actor))
			}
			assertFortressTaxBalances(t, d, fortressTaxTestGold, fortressTaxTestTreasury)
		})
	}
}

/*
================
TestFortressTaxCollectNpcAdmissionPrecedesAuthority
================
*/
func TestFortressTaxCollectNpcAdmissionPrecedesAuthority(t *testing.T) {
	for _, admission := range []string{"wrong service", "not selected", "other selected", "out of range"} {
		t.Run(admission, func(t *testing.T) {
			d := fortressTaxFixture(t)
			switch admission {
			case "wrong service":
				d.rt.NpcRoster[0].Services = simulation.NpcServices(0).With(simulation.NpcServiceFortressAide)
			case "not selected":
				d.rt.Selected.Clear(testDivision, d.character.Name)
			case "other selected":
				d.rt.Selected.Set(testDivision, d.character.Name, fortressTaxTestNPC+1)
			case "out of range":
				moveMerchantAway(d.rt, npcHitRange+10)
			}
			for _, closed := range []bool{false, true} {
				d.rt.Fortresses.SetPeriod(testDivision, fortress.PeriodTax, !closed)
				out := d.rt.HandleFortressInteraction(testDivision, d.character, fortressTaxCollectPayload(fortressTaxTestID, 100))
				want := []byte{siege.ActionTaxCollect, 2, fortressErrInvalid}
				if len(out.Frames) != 1 || out.Frames[0].Opcode != opFortressInteractionResult || !bytes.Equal(out.Frames[0].Payload, want) {
					t.Fatalf("closed=%v NPC admission = %+v, want %x", closed, out.Frames, want)
				}
				assertFortressTaxBalances(t, d, fortressTaxTestGold, fortressTaxTestTreasury)
			}
		})
	}
}

/*
================
TestFortressTaxCollectMalformedDoesNotMutate
================
*/
func TestFortressTaxCollectMalformedDoesNotMutate(t *testing.T) {
	d := fortressTaxFixture(t)
	valid := fortressTaxCollectPayload(fortressTaxTestID, 100)
	for length := 0; length < len(valid); length++ {
		t.Run(fmt.Sprintf("truncated-%d", length), func(t *testing.T) {
			out := d.rt.HandleFortressInteraction(testDivision, d.character, valid[:length])
			if len(out.Frames) != 0 {
				t.Fatalf("malformed collection emitted %+v", out.Frames)
			}
			assertFortressTaxBalances(t, d, fortressTaxTestGold, fortressTaxTestTreasury)
		})
	}
	out := d.rt.HandleFortressInteraction(testDivision, d.character, append(append([]byte(nil), valid...), 0))
	if len(out.Frames) != 0 {
		t.Fatalf("trailing data accepted: %+v", out.Frames)
	}
	out = d.rt.HandleFortressInteraction(testDivision, nil, valid)
	if len(out.Frames) != 0 {
		t.Fatalf("missing actor accepted: %+v", out.Frames)
	}
	assertFortressTaxBalances(t, d, fortressTaxTestGold, fortressTaxTestTreasury)
}
