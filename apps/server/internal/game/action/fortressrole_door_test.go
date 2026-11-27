/*
===========================================================================

fortressrole_door_test.go - the guild fortress role is read outside every door

The store's guild door takes the store lock a character door already holds,
and that RWMutex is not re-entrant: a guild member's instant self effect
(an imbue, a self buff) read the role inside its character update and hung
r204n's GameWorld twice (tick stalls 2026-11-27 09:06 and 09:08). These
tests fail when any admission reads the guild door with a door open.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
doorWatchGuilds

A one-guild store that records a read made while a character door is
open (the fixture's UpdateCharacter sets open around each update).
================
*/
type doorWatchGuilds struct {
	fortressGuilds
	open      *bool
	readsOpen *int
	reads     *int
}

/*
================
doorWatchGuilds.Guild
================
*/
func (g doorWatchGuilds) Guild(division string, id int64) (domain.GuildRecord, []domain.GuildMemberRecord, bool) {
	*g.reads++
	if *g.open {
		*g.readsOpen++
	}
	return g.fortressGuilds.Guild(division, id)
}

/*
================
watchGuildDoor

Puts c in a guild with a fortress role and wraps the runtime's character
door so a guild read inside it is counted.
================
*/
func watchGuildDoor(t *testing.T, rt *Runtime, c *enterworld.Character) (reads, readsOpen *int) {
	t.Helper()
	guildID := int64(77)
	c.GuildID = &guildID
	open, inside, total := false, 0, 0
	rt.Guilds = doorWatchGuilds{
		fortressGuilds: fortressGuilds{
			guild:   domain.GuildRecord{ID: guildID, Level: 3},
			members: []domain.GuildMemberRecord{{CharID: c.ID, Name: c.Name, FortressRole: fortressRoleCommander}},
		},
		open: &open, readsOpen: &inside, reads: &total,
	}
	rt.deps.(*enterworld.Deps).UpdateCharacter = func(_ *enterworld.Character, _ string, update func() bool) bool {
		open = true
		defer func() { open = false }()
		return update()
	}
	return &total, &inside
}

/*
================
TestGuildMemberImbueReadsTheRoleOutsideTheDoor
================
*/
func TestGuildMemberImbueReadsTheRoleOutsideTheDoor(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	row := installFireImbue(t, rt, c)
	reads, readsOpen := watchGuildDoor(t, rt, c)

	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: row.ID}.Encode())
	if result.DiagnosticRefusal != "" {
		t.Fatalf("imbue refused: %s", result.DiagnosticRefusal)
	}
	if *readsOpen != 0 {
		t.Fatalf("guild door read %d time(s) inside the character door (deadlocks the store)", *readsOpen)
	}
	if *reads == 0 {
		t.Fatal("the fortress role was never read: the equipment phase lost its role")
	}
	if imbue, _ := rt.activeWeaponImbue(testDivision, c.Name, rt.Now().UnixMilli()); !imbue.Pinned {
		t.Fatal("imbue not active after the cast")
	}
}
