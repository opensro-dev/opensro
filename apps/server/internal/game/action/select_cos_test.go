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
TestSelectingACompanionTakesTheReadDoorOnce

HandleObjectSelect holds the character read door while it resolves the
target; the companion lookup inside it must not take the door again. The
store's door is a sync.RWMutex: a second RLock queued behind a waiting
writer (the tick's UpdateCharacter) never returns, which froze GameWorld
on 2026-10-10. The strict door below fails on any nesting.
================
*/
func TestSelectingACompanionTakesTheReadDoorOnce(t *testing.T) {
	c, refs := persistentSummonFixture()
	rt, _ := newTestRuntime(c, refs)
	rt.CompanionRoll = func() (uint32, error) { return 0, nil }
	rt.BindPetSession(testDivision, c, 101)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pets := rt.CompanionPresentations(testDivision, c.Name)
	if len(pets) == 0 {
		t.Fatal("no summoned companion")
	}
	deps := rt.deps.(*enterworld.Deps)
	prior := deps.ReadCharacter
	depth, deepest := 0, 0
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
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, pets[0].Row.Gid)
	if out := rt.HandleObjectSelect(testDivision, c, payload); out.Refusal != "" {
		t.Fatalf("companion select refused: %q", out.Refusal)
	}
	if deepest != 1 {
		t.Fatalf("the select took the read door %d deep", deepest)
	}
}

/*
================
companionGuildReadGuard

Guilds and characters share the production Store mutex. Guard both interfaces
so a guild lookup cannot hide recursive locking from a ReadCharacter counter.
================
*/
type companionGuildReadGuard struct {
	enterworld.GuildStore
	t          *testing.T
	depth      int
	reads      int
	guildReads int
}

/*
================
companionGuildReadGuard.Read
================
*/
func (g *companionGuildReadGuard) Read(_ string, read func()) {
	g.t.Helper()
	if g.depth != 0 {
		g.t.Fatal("nested character read door")
	}
	g.depth++
	g.reads++
	defer func() { g.depth-- }()
	read()
}

/*
================
companionGuildReadGuard.Guild
================
*/
func (g *companionGuildReadGuard) Guild(division string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	g.t.Helper()
	if g.depth != 0 {
		g.t.Fatal("Guild called inside character read door: production reenters Store.mu")
	}
	g.guildReads++
	return g.GuildStore.Guild(division, id)
}

/*
================
TestGuildedCompanionDoesNotReenterStore

Selection needs only the companion's live pose and health; public mercenary
presentation also needs the guild name, resolved outside the character door.
================
*/
func TestGuildedCompanionDoesNotReenterStore(t *testing.T) {
	for _, tc := range []struct {
		name      string
		band      uint16
		selectPet bool
	}{
		{name: "select-attack-pet", band: attackPetBand, selectPet: true},
		{name: "select-mercenary", band: domain.MercenaryBand, selectPet: true},
		{name: "present-mercenary", band: domain.MercenaryBand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testItems())
			equipCombatTestPet(t, rt, c, tc.band)
			rt.BindPetSession(testDivision, c, 101)
			guild := domain.GuildRecord{ID: 41, Name: "CompanionGuard"}
			c.GuildID = &guild.ID
			guard := &companionGuildReadGuard{t: t, GuildStore: fortressGuilds{guild: guild}}
			deps := rt.deps.(*enterworld.Deps)
			deps.ReadCharacter = guard.Read
			deps.Guilds = guard
			gid := c.ActiveCOS.GID
			if tc.selectPet {
				payload := make([]byte, 4)
				binary.LittleEndian.PutUint32(payload, gid)
				out := rt.HandleObjectSelect(testDivision, c, payload)
				if out.Refusal != "" || out.Selected != gid {
					t.Fatalf("guilded companion select = %+v, want gid %d", out, gid)
				}
				frame, ok := findFrame(out.Frames, wire.OpObjectSelectResult)
				if !ok || len(frame.Payload) != 14 || binary.LittleEndian.Uint32(frame.Payload[6:]) != c.ActiveCOS.CurrentHP {
					t.Fatalf("guilded companion health grant = %x", frame.Payload)
				}
				if guard.guildReads != 0 {
					t.Fatalf("selection queried guild decoration %d times", guard.guildReads)
				}
			} else {
				pets := rt.CompanionPresentations(testDivision, c.Name)
				if len(pets) != 1 || pets[0].Row.Gid != gid || pets[0].Row.Band != domain.MercenaryBand || pets[0].Row.OwnerName != guild.Name {
					t.Fatalf("mercenary presentation lost guild decoration: %+v", pets)
				}
				if guard.guildReads != 1 {
					t.Fatalf("public presentation queried guild %d times, want 1", guard.guildReads)
				}
			}
			if guard.reads == 0 || guard.depth != 0 {
				t.Fatalf("character read guard: reads=%d depth=%d", guard.reads, guard.depth)
			}
		})
	}
}
