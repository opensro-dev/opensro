/*
===========================================================================

select_cos_test.go - selecting a summoned companion

CGObjPC_HandleSelectRequest0x7045 (52B040) grants any character in hit
range, and CGObjCOS inherits the NPC/monster select writer, so a companion
answers with the non-user arm carrying its current HP.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestSelectingAnOwnCompanionGrantsItsHealth

A summoned pet is selectable by its owner; the grant is the 14-byte arm
with the pet's current HP, and the selection is recorded.
================
*/
func TestSelectingAnOwnCompanionGrantsItsHealth(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pets := rt.CompanionPresentations(testDivision, c.Name)
	if len(pets) == 0 {
		t.Fatal("no summoned companion")
	}
	gid := pets[0].Row.Gid
	record := c.CompanionByGID(gid)
	if record == nil {
		t.Fatal("companion record missing")
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, gid)
	out := rt.HandleObjectSelect(testDivision, c, payload)
	if out.Refusal != "" || out.Selected != gid {
		t.Fatalf("companion select refused: %q", out.Refusal)
	}
	frame, ok := findFrame(out.Frames, wire.OpObjectSelectResult)
	if !ok || len(frame.Payload) != 14 || frame.Payload[0] != 1 || frame.Payload[5] != 1 {
		t.Fatalf("companion grant %x", frame.Payload)
	}
	if binary.LittleEndian.Uint32(frame.Payload[1:]) != gid || binary.LittleEndian.Uint32(frame.Payload[6:]) != record.CurrentHP {
		t.Fatalf("grant names %x, want gid %d and HP %d", frame.Payload, gid, record.CurrentHP)
	}
}

/*
================
doorGuilds

A guild authority that records a lookup made while the character read
door is held: the store's guild door takes the same RWMutex.
================
*/
type doorGuilds struct {
	domain.GuildStore
	depth    *int
	inDoor   *bool
	lookedUp *int
}

func (g doorGuilds) Guild(_ string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	*g.lookedUp++
	if *g.depth > 0 {
		*g.inDoor = true
	}
	return domain.GuildRecord{ID: id, Name: "DoorGuild"}, nil, true
}

/*
================
TestSelectingACompanionTakesNoNestedStoreDoor

HandleObjectSelect holds the character read door while it resolves the
target; the companion lookup inside it must reach no store door again,
neither the character door nor the guild door (both share the store's
RWMutex). A second RLock queued behind a waiting writer (the tick's
UpdateCharacter) never returns, which froze GameWorld on 2026-10-10. The
owner is in a guild so the guild path is exercised; outside a door the
presentation still resolves the guild name.
================
*/
func TestSelectingACompanionTakesNoNestedStoreDoor(t *testing.T) {
	c, refs := persistentSummonFixture()
	guild := int64(77)
	c.GuildID = &guild
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	deps := rt.deps.(*enterworld.Deps)
	depth, deepest, lookedUp := 0, 0, 0
	inDoor := false
	deps.Guilds = doorGuilds{depth: &depth, inDoor: &inDoor, lookedUp: &lookedUp}
	pets := rt.CompanionPresentations(testDivision, c.Name)
	if len(pets) == 0 {
		t.Fatal("no summoned companion")
	}
	if lookedUp == 0 {
		t.Fatal("the guilded owner's name was never looked up")
	}
	prior := deps.ReadCharacter
	deps.ReadCharacter = func(division string, read func()) {
		depth++
		deepest = max(deepest, depth)
		defer func() { depth-- }()
		if prior != nil {
			prior(division, read)
			return
		}
		read()
	}
	inDoor = false
	pets = rt.CompanionPresentations(testDivision, c.Name)
	if inDoor {
		t.Fatal("the presentation looked the guild up inside the read door")
	}
	deepest = 0
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, pets[0].Row.Gid)
	if out := rt.HandleObjectSelect(testDivision, c, payload); out.Refusal != "" {
		t.Fatalf("companion select refused: %q", out.Refusal)
	}
	if deepest != 1 || inDoor {
		t.Fatalf("the select took the read door %d deep (guild lookup in the door: %v)", deepest, inDoor)
	}
}
