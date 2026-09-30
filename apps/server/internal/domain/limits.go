package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

/*
================================================================================
Native authority limits

These are persistence invariants, not merely encoder conveniences. Keeping
them in the domain prevents the store and gameplay packages from inventing
different ceilings for identities or strings that share one native wire.
================================================================================
*/

const (
	// MaxCharacterID is the largest character identity that remains unique in
	// both the u32 social fields and PlayerObjectID's signed-31-bit input.
	MaxCharacterID int64 = 0x7fffffff
	// MaxGuildID is the largest identity carried by the guild u32.
	MaxGuildID int64 = 0xffffffff

	// ReservedAccountID is an internal sentinel. It may never authenticate or
	// own a newly created live character.
	ReservedAccountID = "__open_dev__"

	AccountIDMaxBytes     = 64
	CharacterNameMinBytes = 2
	CharacterNameMaxBytes = 12
	// MaxCharactersPerShardAccount is the retail character-select roster
	// ceiling. Delete-pending characters still occupy their slot until the
	// reservation matures and the record is archived.
	MaxCharactersPerShardAccount = 4

	GuildNameMaxBytes       = 12
	GuildNoticeSubjectBytes = 127
	GuildNoticeBodyBytes    = 1023
	GuildGrantNameMaxBytes  = GuildNameMaxBytes
	GuildMemberMaxCount     = 255

	LetterBodyMaxBytes    = 0x100
	LetterMailboxMaxCount = 0x14
)

/*
================
FoldAccountID

The case-folded form under which two account ids are the same login:
ASCII letters only, exactly as SQLite's lower() folds them, so it agrees
with the account store's unique accounts_folded_id index. Retail logins
(an MSSQL account table under a case-insensitive collation) did not
distinguish "Bob" from "bob"; neither does this.
================
*/
func FoldAccountID(id string) string {
	folded := []byte(id)
	for i, c := range folded {
		if 'A' <= c && c <= 'Z' {
			folded[i] = c + ('a' - 'A')
		}
	}
	return string(folded)
}

// AccountIDValid is the shared storage/login identity shape. Account IDs are
// opaque and Unicode-capable, but whitespace padding, invalid UTF-8, control
// characters, and overlong values are never valid map or ownership keys.
func AccountIDValid(value string) bool {
	if value == "" || strings.TrimSpace(value) != value ||
		len(value) > AccountIDMaxBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
