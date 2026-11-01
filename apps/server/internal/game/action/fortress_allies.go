/*
===========================================================================

fortress_allies.go - which guilds stand with a fortress's holder

A guild is allied with a fortress when it stands in the union of the
guild that holds it (the reference server compares the guild's alliance
id with the holder's ally key). Allies enter the fortress's portals, count and revive as
defenders, apply as allies and may not strike the holder's structures.

===========================================================================
*/
package action

import "opensro.online/server/internal/game/world/fortress"

/*
================
alliedWithFortress
================
*/
func (rt *Runtime) alliedWithFortress(division string, record fortress.Record, guildID int64) bool {
	holder := record.Holder()
	return rt.Unions != nil && rt.Unions.Allied(division, holder, guildID)
}

/*
================
fortressDefender

The holder or one of its allies.
================
*/
func (rt *Runtime) fortressDefender(division string, record fortress.Record, guildID int64) bool {
	if guildID == 0 {
		return false
	}
	return guildID == record.Holder() || rt.alliedWithFortress(division, record, guildID)
}
