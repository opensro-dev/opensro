/*
===========================================================================

capabilities.go - what a staff grant may do, one row per GM power

Port-only, not native; the default by owner decision 2026-11-23. The
original has one GM bit (CICPlayer+0x1890 bit 0, our Character.GMPrivilege)
that opens every GM command. Staff grants split that power: each
capability below is one console checkbox and one bit of
Character.GMCapabilities, and the GM command lane checks the bit at
command time, so a revoke or an unticked box applies to the next command
without a relog.

This table is the only list. The console renders its checkboxes from it
(the operator endpoint serves it), the gate in gmcommand maps every
subcommand onto it, and a test fails when a subcommand has no row.

An Elevated capability can be granted only by a console approver (the
owner); an operator who sets one is refused by the book, never only by
the console's UI.

===========================================================================
*/
package staff

import (
	"fmt"
	"math/bits"

	"opensro.online/server/internal/domain"
)

/*
================
Capability

One bit of Character.GMCapabilities.
================
*/
type Capability uint32

const (
	FindUser Capability = 1 << iota
	TeleportSelf
	Speed
	Invisible
	Invincible
	MovePlayers
	Ban
	WorldControl
	MakeItem
	SpawnMonster
	KillMonster
	GrantSilk
	SetLevel
	Forge
	Berserk
	Quests
	Siege
	// Mute and HoldLogout are the in-game moderation commands (#4207,
	// gmcommand/moderation.go). New capabilities append here so a
	// stored bit never changes meaning.
	Mute
	HoldLogout
	capabilityEnd
)

// All is every capability: what the host allowlist (SRO_GM_CHARACTERS)
// grants, since a host-trusted GM predates the split.
const All = capabilityEnd - 1

/*
================
Row

One capability as the console shows it.
================
*/
type Row struct {
	Capability Capability `json:"-"`
	ID         string     `json:"id"`
	Label      string     `json:"label"`
	Group      string     `json:"group"`
	Elevated   bool       `json:"elevated,omitempty"`
}

// Rows is the capability table, in console order.
var Rows = []Row{
	{FindUser, "find_user", "Find user, real name", "Players", false},
	{MovePlayers, "move_players", "Recall or send players and guilds", "Players", false},
	{Ban, "ban", "Ban", "Players", false},
	{Mute, "mute", "Mute and shadow-mute chat", "Players", false},
	{HoldLogout, "hold_logout", "Block or allow a player's logout", "Players", false},
	{TeleportSelf, "teleport", "Warp and move to user, NPC or town", "Movement", false},
	{Speed, "speed", "Set speed", "Movement", false},
	{Invisible, "invisible", "Invisible", "Visibility", false},
	{Invincible, "invincible", "Invincible", "Visibility", false},
	{WorldControl, "world", "World status, time, flags, instances", "World", false},
	{MakeItem, "make_item", "Make item", "Economy", true},
	{SpawnMonster, "spawn_monster", "Spawn monsters (they drop loot)", "Economy", false},
	{KillMonster, "kill_monster", "Kill monsters, ZOE (rewards)", "Economy", false},
	{GrantSilk, "grant_silk", "Grant Silk", "Economy", false},
	{SetLevel, "set_level", "Grant EXP and levels", "Economy", false},
	{Forge, "forge", "Forge and enhance items", "Economy", false},
	{Berserk, "berserk", "Set Berserk gauge and Blue Berserk level", "Economy", false},
	{Quests, "quests", "Start, reset, complete or remove quests", "Economy", false},
	{Siege, "siege", "Fortress structures and siege period", "Economy", false},
}

/*
================
Allows

Whether the character may use the capability now: a GM whose projected
capability bits carry it. Reads only the character, so every gate that
already holds the record asks it without a new dependency.
================
*/
func Allows(c *domain.Character, capability Capability) bool {
	return c != nil && c.GMPrivilege && Capability(c.GMCapabilities)&capability == capability && capability != 0
}

/*
================
Mask

The bits for capability ids. An unknown id refuses the whole set, so a
typo can never be read as "fewer powers" or "more".
================
*/
func Mask(ids []string) (Capability, error) {
	var mask Capability
	for _, id := range ids {
		row, ok := rowByID(id)
		if !ok {
			return 0, fmt.Errorf("staff: unknown capability %q", id)
		}
		mask |= row.Capability
	}
	return mask, nil
}

/*
================
IDs

The capability ids a mask holds, in table order.
================
*/
func IDs(mask Capability) []string {
	ids := make([]string, 0, bits.OnesCount32(uint32(mask)))
	for _, row := range Rows {
		if mask&row.Capability != 0 {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

/*
================
Elevated

The elevated bits of a mask.
================
*/
func Elevated(mask Capability) Capability {
	var elevated Capability
	for _, row := range Rows {
		if row.Elevated {
			elevated |= row.Capability
		}
	}
	return mask & elevated
}

func rowByID(id string) (Row, bool) {
	for _, row := range Rows {
		if row.ID == id {
			return row, true
		}
	}
	return Row{}, false
}
