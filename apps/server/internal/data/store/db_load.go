/*
===========================================================================

db_load.go - validate the complete durable authority graph before adoption

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"opensro.online/server/internal/domain"
)

// Database loading validates the complete authority graph before Store adopts
// it. Invalid ownership or social/group references fail boot closed.
/*
================
loadDB
================
*/
func loadDB(db *sql.DB, current int, currentLayout int) (*loadedDB, error) {
	version, err := readMetaInt(db, metaKeySchemaVersion, 0)
	if err != nil {
		return nil, err
	}
	if version != current {
		return nil, fmt.Errorf("database character schema version %d, binary requires exactly %d: %w; preserve the authority; schemas %d through the previous version are validated and upgraded offline with sro-authority-upgrade", version, current, errIncompatibleSchema, UpgradeFromVersion)
	}
	layout, err := readMetaInt(db, metaKeyLayoutVersion, 0)
	if err != nil {
		return nil, err
	}
	if layout != currentLayout {
		return nil, fmt.Errorf("database layout version %d, binary requires exactly %d: %w; preserve the authority; supported schemas starting at %d are validated and upgraded offline with sro-authority-upgrade", layout, currentLayout, errIncompatibleSchema, UpgradeFromVersion)
	}

	// Only the offline upgrader requests layout 4. Runtime callers require
	// CurrentLayoutVersion and never mutate an older authority during boot.
	if currentLayout >= 5 {
		if err := validateAccountStorages(db); err != nil {
			return nil, err
		}
		if err := validateMallAccounts(db); err != nil {
			return nil, fmt.Errorf("validating mall accounts: %w", err)
		}
	}

	out := &loadedDB{
		characters:   map[string][]*domain.Character{},
		deleted:      map[string][]json.RawMessage{},
		ground:       map[string][]domain.GroundItemRecord{},
		mailboxes:    map[string]map[int64][]domain.LetterRecord{},
		guilds:       map[string]map[int64]domain.GuildRecord{},
		guildMembers: map[string]map[int64][]domain.GuildMemberRecord{},
		camps:        map[string]map[int64]domain.TrainingCampRecord{},
		campMembers:  map[string]map[int64][]domain.TrainingCampMemberRecord{},
		meta:         Meta{NextCharID: map[string]int64{}, NextGuildID: map[string]int64{}},
	}

	out.version = current

	rows, err := db.Query("SELECT division, id, name_lower, record FROM characters ORDER BY division, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var division, nameLower, record string
		var id int64
		if err := rows.Scan(&division, &id, &nameLower, &record); err != nil {
			return nil, err
		}
		c, err := decodeCharacterStrict(json.RawMessage(record))
		if err != nil {
			return nil, fmt.Errorf("division %s character %d record: %w", division, id, err)
		}
		if c.ID != id {
			return nil, fmt.Errorf("division %s character table id %d disagrees with record id %d", division, id, c.ID)
		}
		if strings.ToLower(c.Name) != nameLower {
			return nil, fmt.Errorf("division %s character %d name index %q disagrees with record name %q", division, id, nameLower, c.Name)
		}
		out.characters[division] = append(out.characters[division], c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	delRows, err := db.Query("SELECT division, record FROM deleted_characters ORDER BY division, seq")
	if err != nil {
		return nil, err
	}
	defer delRows.Close()
	for delRows.Next() {
		var division, record string
		if err := delRows.Scan(&division, &record); err != nil {
			return nil, err
		}
		out.deleted[division] = append(out.deleted[division], json.RawMessage(record))
	}
	if err := delRows.Err(); err != nil {
		return nil, err
	}

	groundRows, err := db.Query("SELECT division, record FROM ground_items ORDER BY division, gid")
	if err != nil {
		return nil, err
	}
	defer groundRows.Close()
	for groundRows.Next() {
		var division, record string
		if err := groundRows.Scan(&division, &record); err != nil {
			return nil, err
		}
		item := domain.GroundItemRecord{}
		if err := decodeJSONStrict([]byte(record), &item); err != nil {
			return nil, fmt.Errorf("division %s ground record: %w", division, err)
		}
		out.ground[division] = append(out.ground[division], item)
	}
	if err := groundRows.Err(); err != nil {
		return nil, err
	}

	// Mailboxes are owned by the memos side table and loaded in wire order.
	memoRows, err := db.Query("SELECT division, char_id, record FROM memos ORDER BY division, char_id, seq")
	if err != nil {
		return nil, err
	}
	defer memoRows.Close()
	for memoRows.Next() {
		var division, record string
		var charID int64
		if err := memoRows.Scan(&division, &charID, &record); err != nil {
			return nil, err
		}
		letter := domain.LetterRecord{}
		if err := decodeJSONStrict([]byte(record), &letter); err != nil {
			return nil, fmt.Errorf("division %s char %d memo record: %w", division, charID, err)
		}
		if out.mailboxes[division] == nil {
			out.mailboxes[division] = map[int64][]domain.LetterRecord{}
		}
		out.mailboxes[division][charID] = append(out.mailboxes[division][charID], letter)
	}
	if err := memoRows.Err(); err != nil {
		return nil, err
	}

	// Guild rows and ordered membership rows are separate persistence
	// aggregates joined by division and guild id.
	guildRows, err := db.Query("SELECT division, guild_id, record FROM guilds ORDER BY division, guild_id")
	if err != nil {
		return nil, err
	}
	defer guildRows.Close()
	for guildRows.Next() {
		var division, record string
		var guildID int64
		if err := guildRows.Scan(&division, &guildID, &record); err != nil {
			return nil, err
		}
		guild := domain.GuildRecord{}
		if err := decodeJSONStrict([]byte(record), &guild); err != nil {
			return nil, fmt.Errorf("division %s guild %d record: %w", division, guildID, err)
		}
		if guild.ID != guildID {
			return nil, fmt.Errorf("division %s guild table id %d disagrees with record id %d", division, guildID, guild.ID)
		}
		if out.guilds[division] == nil {
			out.guilds[division] = map[int64]domain.GuildRecord{}
		}
		out.guilds[division][guildID] = guild
	}
	if err := guildRows.Err(); err != nil {
		return nil, err
	}

	memberRows, err := db.Query("SELECT division, guild_id, char_id, record FROM guild_members ORDER BY division, guild_id, seq")
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var division, record string
		var guildID, charID int64
		if err := memberRows.Scan(&division, &guildID, &charID, &record); err != nil {
			return nil, err
		}
		member := domain.GuildMemberRecord{}
		if err := decodeJSONStrict([]byte(record), &member); err != nil {
			return nil, fmt.Errorf("division %s guild %d member record: %w", division, guildID, err)
		}
		if member.CharID != charID {
			return nil, fmt.Errorf("division %s guild %d member table character id %d disagrees with record id %d", division, guildID, charID, member.CharID)
		}
		if out.guildMembers[division] == nil {
			out.guildMembers[division] = map[int64][]domain.GuildMemberRecord{}
		}
		out.guildMembers[division][guildID] = append(out.guildMembers[division][guildID], member)
	}
	if err := memberRows.Err(); err != nil {
		return nil, err
	}

	// Training camps (the training_camps + training_camp_members tables).
	// Same side-table posture as guilds: covered by the LAYOUT version
	// (layoutVersionTrainingCamps) - a pre-camp database gains the empty
	// tables on its first open here and is stamped forward, and an older
	// binary opening a store that carries camps refuses via the layout
	// check instead of silently serving a campless world. No character
	// schema key is involved (camps carry no Character FK - the
	// internal/game/social/mentor DECISION).
	campRows, err := db.Query("SELECT division, camp_id, record FROM training_camps ORDER BY division, camp_id")
	if err != nil {
		return nil, err
	}
	defer campRows.Close()
	for campRows.Next() {
		var division, record string
		var campID int64
		if err := campRows.Scan(&division, &campID, &record); err != nil {
			return nil, err
		}
		camp := domain.TrainingCampRecord{}
		if err := decodeJSONStrict([]byte(record), &camp); err != nil {
			return nil, fmt.Errorf("division %s training camp %d record: %w", division, campID, err)
		}
		if camp.ID != campID {
			return nil, fmt.Errorf("division %s training camp table id %d disagrees with record id %d", division, campID, camp.ID)
		}
		if out.camps[division] == nil {
			out.camps[division] = map[int64]domain.TrainingCampRecord{}
		}
		out.camps[division][campID] = camp
	}
	if err := campRows.Err(); err != nil {
		return nil, err
	}

	campMemberRows, err := db.Query("SELECT division, camp_id, char_id, record FROM training_camp_members ORDER BY division, camp_id, seq")
	if err != nil {
		return nil, err
	}
	defer campMemberRows.Close()
	for campMemberRows.Next() {
		var division, record string
		var campID, charID int64
		if err := campMemberRows.Scan(&division, &campID, &charID, &record); err != nil {
			return nil, err
		}
		member := domain.TrainingCampMemberRecord{}
		if err := decodeJSONStrict([]byte(record), &member); err != nil {
			return nil, fmt.Errorf("division %s training camp %d member record: %w", division, campID, err)
		}
		if member.CharID != charID {
			return nil, fmt.Errorf("division %s training camp %d member table character id %d disagrees with record id %d", division, campID, charID, member.CharID)
		}
		if out.campMembers[division] == nil {
			out.campMembers[division] = map[int64][]domain.TrainingCampMemberRecord{}
		}
		out.campMembers[division][campID] = append(out.campMembers[division][campID], member)
	}
	if err := campMemberRows.Err(); err != nil {
		return nil, err
	}

	idRows, err := db.Query("SELECT division, next_id FROM next_char_id")
	if err != nil {
		return nil, err
	}
	defer idRows.Close()
	for idRows.Next() {
		var division string
		var next int64
		if err := idRows.Scan(&division, &next); err != nil {
			return nil, err
		}
		if next < 1 || next > domain.MaxCharacterID+1 {
			return nil, fmt.Errorf("division %s next character id %d is outside 1..%d", division, next, domain.MaxCharacterID+1)
		}
		out.meta.NextCharID[division] = next
	}
	if err := idRows.Err(); err != nil {
		return nil, err
	}

	// The guild-id watermark (next_char_id's twin). Same side-table
	// posture as the guilds tables themselves: covered by the LAYOUT
	// version (layoutVersionGuilds) - a pre-guild database gains the
	// empty table on its first open (ensureSchema above), and an absent
	// row means "unseeded" exactly like an absent next_char_id row.
	guildIDRows, err := db.Query("SELECT division, next_id FROM next_guild_id")
	if err != nil {
		return nil, err
	}
	defer guildIDRows.Close()
	for guildIDRows.Next() {
		var division string
		var next int64
		if err := guildIDRows.Scan(&division, &next); err != nil {
			return nil, err
		}
		if next < 1 || next > domain.MaxGuildID+1 {
			return nil, fmt.Errorf("division %s next guild id %d is outside 1..%d", division, next, domain.MaxGuildID+1)
		}
		out.meta.NextGuildID[division] = next
	}
	if err := guildIDRows.Err(); err != nil {
		return nil, err
	}

	gid, err := readMetaInt64(db, metaKeyGidCounter, 0)
	if err != nil {
		return nil, err
	}
	if gid < 0 || gid > int64(domain.MaxGroundItemGIDCounter) {
		return nil, fmt.Errorf(
			"ground gid counter %d is outside the allocatable range 0..%d",
			gid,
			domain.MaxGroundItemGIDCounter,
		)
	}
	out.meta.GidCounter = uint32(gid)
	if err := validateLoadedAuthorityGraph(out); err != nil {
		return nil, err
	}
	return out, nil
}

// validateLoadedAuthorityGraph proves that independently stored rows form one
// authority graph before any of it becomes visible. Current-schema data has no
// permissive interpretation: a dangling FK, orphan row, duplicate
// membership, missing leader/master, or delete-pending group member is
// corruption and must take the normal quarantine/bak/refusal ladder.
/*
================
validateLoadedAuthorityGraph
================
*/
func validateLoadedAuthorityGraph(loaded *loadedDB) error {
	for divisionID, rows := range loaded.ground {
		for index, row := range rows {
			if err := validateGroundItemRecord(row, loaded.meta.GidCounter); err != nil {
				return fmt.Errorf("division %s ground item %d: %w", divisionID, index, err)
			}
		}
	}

	characters := make(map[string]map[int64]*domain.Character, len(loaded.characters))
	for divisionID, records := range loaded.characters {
		byID := make(map[int64]*domain.Character, len(records))
		for _, character := range records {
			if err := validateCharacterIdentity(character); err != nil {
				return fmt.Errorf("division %s: %w", divisionID, err)
			}
			if len(character.Masteries) == 0 {
				return fmt.Errorf("division %s character %d has no masteries", divisionID, character.ID)
			}
			if len(character.Skills) == 0 {
				return fmt.Errorf("division %s character %d has no racial base skills", divisionID, character.ID)
			}
			if _, duplicate := byID[character.ID]; duplicate {
				return fmt.Errorf("division %s repeats character id %d", divisionID, character.ID)
			}
			byID[character.ID] = character
		}
		if next, persisted := loaded.meta.NextCharID[divisionID]; persisted {
			var highest int64
			for id := range byID {
				if id > highest {
					highest = id
				}
			}
			if next <= highest {
				return fmt.Errorf("division %s next character id %d does not exceed highest live id %d", divisionID, next, highest)
			}
		}
		characters[divisionID] = byID
	}
	for divisionID, byID := range characters {
		for characterID, character := range byID {
			if len(character.Friends) > domain.FriendMaxCount {
				return fmt.Errorf("division %s character %d has %d friends, client maximum is %d", divisionID, characterID, len(character.Friends), domain.FriendMaxCount)
			}
			seen := make(map[int64]bool, len(character.Friends))
			for _, edge := range character.Friends {
				if edge.ID == characterID {
					return fmt.Errorf("division %s character %d has a self friend edge", divisionID, characterID)
				}
				if seen[edge.ID] {
					return fmt.Errorf("division %s character %d repeats friend %d", divisionID, characterID, edge.ID)
				}
				seen[edge.ID] = true
				target := byID[edge.ID]
				if target == nil {
					return fmt.Errorf("division %s character %d references unknown friend %d", divisionID, characterID, edge.ID)
				}
				if edge.Name != target.Name {
					return fmt.Errorf("division %s character %d friend %d name %q disagrees with character name %q", divisionID, characterID, edge.ID, edge.Name, target.Name)
				}
				reciprocal := false
				for _, reverse := range target.Friends {
					if reverse.ID == characterID && reverse.Name == character.Name {
						reciprocal = true
						break
					}
				}
				if !reciprocal {
					return fmt.Errorf("division %s character %d friend %d has no reciprocal edge", divisionID, characterID, edge.ID)
				}
			}
		}
	}
	for divisionID, mailboxes := range loaded.mailboxes {
		for characterID, mailbox := range mailboxes {
			if characters[divisionID][characterID] == nil {
				return fmt.Errorf("division %s has a mailbox for unknown character %d", divisionID, characterID)
			}
			if len(mailbox) > domain.LetterMailboxMaxCount {
				return fmt.Errorf("division %s character %d has %d letters, client maximum is %d", divisionID, characterID, len(mailbox), domain.LetterMailboxMaxCount)
			}
			for index, letter := range mailbox {
				if err := validateLetterRecord(letter); err != nil {
					return fmt.Errorf("division %s character %d letter %d: %w", divisionID, characterID, index, err)
				}
			}
		}
	}

	for divisionID, guildRows := range loaded.guilds {
		var seenNames []domain.GuildRecord
		seenMembers := map[int64]int64{}
		for guildID, guild := range guildRows {
			if err := validateGuildRecord(guild); err != nil {
				return fmt.Errorf("division %s: %w", divisionID, err)
			}
			for _, other := range seenNames {
				if strings.EqualFold(other.Name, guild.Name) {
					return fmt.Errorf("division %s guilds %d and %d share name %q", divisionID, other.ID, guildID, guild.Name)
				}
			}
			seenNames = append(seenNames, guild)
			members := loaded.guildMembers[divisionID][guildID]
			if len(members) == 0 {
				return fmt.Errorf("division %s guild %d has no members", divisionID, guildID)
			}
			if len(members) > domain.GuildMemberMaxCount {
				return fmt.Errorf("division %s guild %d has %d members, u8 wire maximum is 255", divisionID, guildID, len(members))
			}
			leaders := 0
			seenJIDs := make(map[uint32]bool, len(members))
			for _, member := range members {
				if otherGuild, duplicate := seenMembers[member.CharID]; duplicate {
					return fmt.Errorf("division %s character %d belongs to guilds %d and %d", divisionID, member.CharID, otherGuild, guildID)
				}
				seenMembers[member.CharID] = guildID
				character := characters[divisionID][member.CharID]
				if character == nil {
					return fmt.Errorf("division %s guild %d references unknown character %d", divisionID, guildID, member.CharID)
				}
				if character.DeletePending {
					return fmt.Errorf("division %s guild %d contains delete-pending character %d", divisionID, guildID, member.CharID)
				}
				if member.Name != character.Name {
					return fmt.Errorf("division %s guild %d member %d name %q disagrees with character name %q", divisionID, guildID, member.CharID, member.Name, character.Name)
				}
				if len(member.GrantName) > domain.GuildGrantNameMaxBytes {
					return fmt.Errorf("division %s guild %d member %d grant name exceeds %d bytes", divisionID, guildID, member.CharID, domain.GuildGrantNameMaxBytes)
				}
				if seenJIDs[member.JID] {
					return fmt.Errorf("division %s guild %d repeats member jid %d", divisionID, guildID, member.JID)
				}
				seenJIDs[member.JID] = true
				if character.GuildID == nil || *character.GuildID != guildID {
					return fmt.Errorf("division %s guild %d member %d has character FK %v", divisionID, guildID, member.CharID, character.GuildID)
				}
				if member.Grade == 0 {
					leaders++
				}
			}
			if leaders != 1 {
				return fmt.Errorf("division %s guild %d has %d grade-0 leaders, want exactly one", divisionID, guildID, leaders)
			}
		}
		if next, persisted := loaded.meta.NextGuildID[divisionID]; persisted {
			var highest int64
			for id := range guildRows {
				if id > highest {
					highest = id
				}
			}
			if next <= highest {
				return fmt.Errorf("division %s next guild id %d does not exceed highest live id %d", divisionID, next, highest)
			}
		}
		for guildID := range loaded.guildMembers[divisionID] {
			if _, ok := guildRows[guildID]; !ok {
				return fmt.Errorf("division %s has member rows for unknown guild %d", divisionID, guildID)
			}
		}
		for _, character := range characters[divisionID] {
			if character.GuildID == nil {
				continue
			}
			if seenMembers[character.ID] != *character.GuildID {
				return fmt.Errorf("division %s character %d points to guild %d without a matching member row", divisionID, character.ID, *character.GuildID)
			}
		}
	}
	for divisionID, membersByGuild := range loaded.guildMembers {
		if len(membersByGuild) > 0 && len(loaded.guilds[divisionID]) == 0 {
			return fmt.Errorf("division %s has guild member rows without guild rows", divisionID)
		}
	}
	for divisionID, byID := range characters {
		if len(loaded.guilds[divisionID]) != 0 {
			continue
		}
		for _, character := range byID {
			if character.GuildID != nil {
				return fmt.Errorf("division %s character %d points to unknown guild %d", divisionID, character.ID, *character.GuildID)
			}
		}
	}

	for divisionID, campRows := range loaded.camps {
		seenMembers := map[int64]int64{}
		for campID, camp := range campRows {
			if camp.ID != camp.MasterCharID {
				return fmt.Errorf("division %s training camp %d has master id %d", divisionID, campID, camp.MasterCharID)
			}
			members := loaded.campMembers[divisionID][campID]
			if len(members) == 0 {
				return fmt.Errorf("division %s training camp %d has no members", divisionID, campID)
			}
			if len(members) > 8 {
				return fmt.Errorf("division %s training camp %d has %d members, client maximum is 8", divisionID, campID, len(members))
			}
			masters := 0
			for _, member := range members {
				if member.Kind > 2 {
					return fmt.Errorf("division %s training camp %d member %d has invalid kind %d", divisionID, campID, member.CharID, member.Kind)
				}
				if otherCamp, duplicate := seenMembers[member.CharID]; duplicate {
					return fmt.Errorf("division %s character %d belongs to training camps %d and %d", divisionID, member.CharID, otherCamp, campID)
				}
				seenMembers[member.CharID] = campID
				character := characters[divisionID][member.CharID]
				if character == nil {
					return fmt.Errorf("division %s training camp %d references unknown character %d", divisionID, campID, member.CharID)
				}
				if character.DeletePending {
					return fmt.Errorf("division %s training camp %d contains delete-pending character %d", divisionID, campID, member.CharID)
				}
				if member.Kind == 0 {
					if member.CharID != camp.MasterCharID {
						return fmt.Errorf("division %s training camp %d master row names character %d", divisionID, campID, member.CharID)
					}
					masters++
				}
			}
			if masters != 1 {
				return fmt.Errorf("division %s training camp %d has %d master rows, want exactly one", divisionID, campID, masters)
			}
		}
		for campID := range loaded.campMembers[divisionID] {
			if _, ok := campRows[campID]; !ok {
				return fmt.Errorf("division %s has member rows for unknown training camp %d", divisionID, campID)
			}
		}
	}
	for divisionID, membersByCamp := range loaded.campMembers {
		if len(membersByCamp) > 0 && len(loaded.camps[divisionID]) == 0 {
			return fmt.Errorf("division %s has training-camp member rows without camp rows", divisionID)
		}
	}
	return nil
}

// readMeta reads one meta value; ok=false when the key is absent.
/*
================
readMeta
================
*/
func readMeta(db *sql.DB, key string) (string, bool, error) {
	var value string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// readMetaInt reads an integer meta value with a caller-selected default.
/*
================
readMetaInt
================
*/
func readMetaInt(db *sql.DB, key string, absent int) (int, error) {
	value, ok, err := readMeta(db, key)
	if err != nil || !ok {
		return absent, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("meta %s = %q: %w", key, value, err)
	}
	return parsed, nil
}

// readMetaInt64 keeps persisted counters independent of the host's int
// width. Schema/layout versions use readMetaInt because they are tiny.
/*
================
readMetaInt64
================
*/
func readMetaInt64(db *sql.DB, key string, absent int64) (int64, error) {
	value, ok, err := readMeta(db, key)
	if err != nil || !ok {
		return absent, err
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("meta %s = %q: %w", key, value, err)
	}
	return parsed, nil
}
