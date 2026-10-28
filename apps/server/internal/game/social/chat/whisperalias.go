/*
===========================================================================

whisperalias.go - the job-alias whisper switch (port-only, not native)

The reference server resolves a whisper target by character name alone:
its whisper handler makes a single name-keyed lookup and never consults
the job alias, so whispering a suited player's alias fails there exactly
like whispering a stranger's name. Every name a viewer can see should
still be a name the server can route to, so this port resolves job
aliases in whispers: with the switch on, a typed whisper target that
matches no character name resolves to the online, currently dressed
owner of that job alias (handleWhisper), and the whisper block list
also matches a dressed sender's alias.

The switch ships ON (owner, 2026-10-28). Setting SRO_WHISPER_JOB_ALIAS
to "off" restores the native name-only behaviour in every arm. The
alias/name collision refusals are NOT behind this switch: an alias may
never equal a character name and a new name may never equal an existing
alias. The reference server permits that clash, which lets a suited
player speak under another player's real name - a native bug we do not
copy (owner, 2026-10-28), refused unconditionally in the job-alias and
character-creation paths.

===========================================================================
*/
package chat

import (
	"os"
	"strings"
)

// EnvWhisperJobAlias gates the port-only job-alias whisper rules above.
const EnvWhisperJobAlias = "SRO_WHISPER_JOB_ALIAS"

/*
================
WhisperJobAliasEnabled

On unless explicitly switched off: "off", "0" or "false" in any casing
restores the native name-only whisper behaviour; anything else,
including unset, is the owner-chosen default on.
================
*/
func WhisperJobAliasEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvWhisperJobAlias))) {
	case "off", "0", "false":
		return false
	}
	return true
}
