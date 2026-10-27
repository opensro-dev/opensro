/*
===========================================================================

noassimilation.go - stones without assimilation (port-only, not native)

Natively a successful stone may then assimilate (the native assimilation
step, per stone kind): with the stone's assimilation chance (its Plus)
one white stat is rerolled, or a magic option. The owner
approved removing it as a server setting (2026-10-13): with
SRO_ALCHEMY_NO_ASSIMILATION on, a successful attribute or magic stone
changes only its own stat and nothing else on the item moves. Unset or
"off" is native.

===========================================================================
*/

package alchemy

import (
	"fmt"
	"os"
	"strings"
)

// EnvNoAssimilation names the setting: "on" or "off".
const EnvNoAssimilation = "SRO_ALCHEMY_NO_ASSIMILATION"

/*
================
ParseNoAssimilation

"" and "off" are native (false); "on" removes stone assimilation. A
malformed value is an error so a typo never ships the other rule silently.
================
*/
func ParseNoAssimilation(text string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "off", "0", "false":
		return false, nil
	case "on", "1", "true":
		return true, nil
	}
	return false, fmt.Errorf("%s: %q is not on or off", EnvNoAssimilation, text)
}

/*
================
NoAssimilationFromEnv
================
*/
func NoAssimilationFromEnv() (bool, error) {
	return ParseNoAssimilation(os.Getenv(EnvNoAssimilation))
}
