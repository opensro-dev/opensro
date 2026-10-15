/*
===========================================================================

identity.go - the constant names of a bot account and a bot character

The operator identifies a bot by one constant prefix. The native
character-name shape (letters, digits, underscore; 2 to 12 characters)
bounds the prefix, so the derived names stay legal.

===========================================================================
*/
package bot

import (
	"fmt"
	"strings"
)

// NamePrefix is the constant prefix of every bot account and character. The
// operator finds a bot by this prefix in `make chars` and in the telemetry.
// The native name shape (letters, digits, underscore) allows the underscore.
const NamePrefix = "bot_"

// MaxCount bounds the number of bots. The operator raises it on purpose.
const MaxCount = 100

// AccountID returns the account id of bot number index. The id is 8
// characters and inside the account-id bound of 64 bytes.
func AccountID(index int) string {
	return fmt.Sprintf("%s%04d", NamePrefix, index)
}

// CharacterName returns the character name of bot number index. The name is
// 8 characters, inside the native 2 to 12 bound.
func CharacterName(index int) string {
	return fmt.Sprintf("%s%04d", NamePrefix, index)
}

// IsBot reports whether name carries the bot prefix.
func IsBot(name string) bool {
	return strings.HasPrefix(name, NamePrefix)
}
