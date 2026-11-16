/*
===========================================================================

guild_views.go - the guild roster's published member views

The guild door's read side of membership: stored member rows carry no
character level (guildMembersForStorage strips it), and the views a
reader gets project each member's live level under the store lock. The
projection is memoised per guild (#3291) and republished only when the
stored rows or a member's level move. guilds.go owns the door and its
writes; this file owns only what a member row becomes on the way out.

===========================================================================
*/
package store

import "opensro.online/server/internal/domain"

// guildMemberViewsLocked projects character-owned roster fields while the
// store lock keeps membership and character state coherent. Stored guild rows
// deliberately carry no mutable character level.
//
// The projection is memoised per guild (#3291): every call re-reads the
// stored rows and each member's live level, and the published copy is
// served again only while both stand exactly as published; any roster or
// level movement republishes. A published view is never mutated, only
// replaced, so a reader racing a republish keeps the previous consistent
// view. The result is the store's published copy: callers must not store
// it beyond the read or mutate its elements (the statuseffect snapshot
// contract; the guild vote path copies before it edits, guildvote.go).
/*
================
guildMemberViewsLocked
================
*/
func (door storeGuildDoor) guildMemberViewsLocked(
	divisionID string,
	guildID int64,
	members []domain.GuildMemberRecord,
) ([]domain.GuildMemberRecord, bool) {
	key := guildKey{division: divisionID, guildID: guildID}
	door.s.guildViewMu.Lock()
	published, ok := door.s.guildViews[key]
	door.s.guildViewMu.Unlock()
	if ok && len(published) == len(members) &&
		door.guildViewsCurrentLocked(published, divisionID, members) {
		return published, true
	}
	views := make([]domain.GuildMemberRecord, len(members))
	for index, member := range members {
		character := door.s.characterByIDLocked(divisionID, member.CharID)
		if character == nil {
			return nil, false
		}
		member.Level = guildMemberLevel(character)
		views[index] = member
	}
	door.s.guildViewMu.Lock()
	if door.s.guildViews == nil {
		door.s.guildViews = make(map[guildKey][]domain.GuildMemberRecord)
	}
	door.s.guildViews[key] = views
	door.s.guildViewMu.Unlock()
	return views, true
}

/*
================
guildViewsCurrentLocked

Whether the published views still project exactly these stored rows:
the same rows in the same order (Level aside - stored rows never
carry one) and every member's live level unmoved. A member whose
character is gone fails the check, as the fresh projection in
guildMemberViewsLocked refuses the same set.
================
*/
func (door storeGuildDoor) guildViewsCurrentLocked(
	published []domain.GuildMemberRecord,
	divisionID string,
	members []domain.GuildMemberRecord,
) bool {
	for index, member := range members {
		character := door.s.characterByIDLocked(divisionID, member.CharID)
		if character == nil {
			return false
		}
		view := published[index]
		if view.Level != guildMemberLevel(character) {
			return false
		}
		view.Level = 0
		if view != member {
			return false
		}
	}
	return true
}

// guildMembersForStorage strips character-owned projections before a member
// set becomes live or durable. This makes accidental stale-level retention
// impossible even in the in-memory aggregate.
/*
================
guildMembersForStorage
================
*/
func guildMembersForStorage(members []domain.GuildMemberRecord) []domain.GuildMemberRecord {
	stored := make([]domain.GuildMemberRecord, len(members))
	for index, member := range members {
		member.Level = 0
		stored[index] = member
	}
	return stored
}

/*
================
guildMemberLevel
================
*/
func guildMemberLevel(character *domain.Character) uint8 {
	level := resolvedCharacterLevel(character)
	if level > 0xff {
		level = 0xff
	}
	return uint8(level)
}
