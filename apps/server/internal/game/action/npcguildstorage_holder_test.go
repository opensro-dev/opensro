/*
===========================================================================

npcguildstorage_holder_test.go - the warehouse holder, guild-lane side

The holder is runtime state in this package; the guild lane's mutators
refuse around him through Runtime.GuildStorageHolder. These tests wire
the real runtime to the real guild handlers over one store: while the
member holds the room, his leave, his expulsion, the guild's dissolve
and the clearing of his storage right all refuse; once he closes the
room, they all run again.

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/social/guild"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGuildWarehouseHolderIsLockedInUntilHeCloses

#4490 U10-04/U10-06 end to end.
================
*/
func TestGuildWarehouseHolderIsLockedInUntilHeCloses(t *testing.T) {
	d := guildManagerFixture(t)
	master := d.character
	heir := testCharacter()
	heir.Name = "Heir"
	if err := d.authority.CreateCharacter(testDivision, "heir-account", heir); err != nil {
		t.Fatal(err)
	}
	guilds := d.authority.Guilds()
	guildID, _ := guilds.GuildOfCharacter(testDivision, master.ID)
	if _, refusal := guilds.AddGuildMemberAs(testDivision, guildID, master.ID, 0, enterworld.GuildMemberRecord{
		CharID: heir.ID, JID: 2, Name: heir.Name, Grade: guild.JoinerGrade, PermMask: guild.PermMaskStorage}); refusal.Refused() {
		t.Fatalf("fixture join %v", refusal)
	}
	if _, refusal := guilds.UpdateGuildAs(testDivision, master.ID, "fixture-level", enterworld.GuildAuthorization{},
		func(g enterworld.GuildRecord, m []enterworld.GuildMemberRecord) (enterworld.GuildRecord, []enterworld.GuildMemberRecord, bool) {
			g.Level = 2
			return g, m, true
		}); refusal.Refused() {
		t.Fatalf("fixture level %v", refusal)
	}

	deps := d.rt.deps.(*enterworld.Deps)
	if _, held := d.rt.GuildStorageHolder(testDivision, guildID); held {
		t.Fatal("the room is held before anyone opens it")
	}
	d.rt.Selected.Set(testDivision, heir.Name, guildManagerGid)
	d.rt.characterAdmissions.Store(simulation.WorldKey(testDivision, heir.Name),
		populationAdmission{division: testDivision, name: heir.Name})
	request := wire.NewWriter(4).U32(guildManagerGid).Payload()
	if out := d.rt.HandleGuildStorageOpen(testDivision, heir, request); !bytes.Equal(out.Frames[0].Payload, []byte{1}) {
		t.Fatalf("the heir's open answered %x", out.Frames[0].Payload)
	}
	holder, held := d.rt.GuildStorageHolder(testDivision, guildID)
	if !held || holder != heir.Name {
		t.Fatalf("holder = %q, %v; want the heir", holder, held)
	}

	// His leave, his expulsion and the dissolve all refuse 0x48.
	gidOnly := wire.NewWriter(4).U32(0).Payload()
	if outcome := guild.HandleLeave(deps, testDivision, heir, gidOnly, holder); !bytes.Equal(outcome.AckPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("the holder's leave answered % X (%s)", outcome.AckPayload, outcome.Refusal)
	}
	kick := wire.NewWriter(2 + len(heir.Name)).U16(uint16(len(heir.Name)))
	kick.Bytes([]byte(heir.Name))
	if outcome := guild.HandleKick(deps, testDivision, master, kick.Payload(), holder); !bytes.Equal(outcome.ErrorPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("expelling the holder answered % X (%s)", outcome.ErrorPayload, outcome.Refusal)
	}
	if outcome := guild.HandleBreak(deps, testDivision, master, gidOnly, holder); !bytes.Equal(outcome.AckPayload, []byte{2, guild.GuildErrStorageHeld}) {
		t.Fatalf("breaking over the holder answered % X (%s)", outcome.AckPayload, outcome.Refusal)
	}
	// And a grant clearing his storage right refuses the whole update.
	grant := wire.NewWriter(9).U8(1).U32(2).U32(guild.PermMaskKick).Payload()
	if outcome := guild.HandlePermissionUpdate(deps, testDivision, master, grant, holder); !bytes.Equal(outcome.ErrorPayload, []byte{2, guild.GuildErrStorageRightHeld}) {
		t.Fatalf("clearing the holder's right answered % X (%s)", outcome.ErrorPayload, outcome.Refusal)
	}
	if _, member := guilds.GuildOfCharacter(testDivision, heir.ID); !member {
		t.Fatal("the holder lost his membership under the refusals")
	}

	// He closes the room; the locks lift and the expulsion lands.
	d.rt.HandleGuildStorageClose(testDivision, heir, request)
	if _, held := d.rt.GuildStorageHolder(testDivision, guildID); held {
		t.Fatal("a closed room still holds")
	}
	kick = wire.NewWriter(2 + len(heir.Name)).U16(uint16(len(heir.Name)))
	kick.Bytes([]byte(heir.Name))
	if outcome := guild.HandleKick(deps, testDivision, master, kick.Payload(), ""); outcome.Refusal != "" {
		t.Fatalf("expelling the closed-out member refused: %s", outcome.Refusal)
	}
	if _, member := guilds.GuildOfCharacter(testDivision, heir.ID); member {
		t.Fatal("the expelled member is still in the guild")
	}
}
