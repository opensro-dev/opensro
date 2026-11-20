/*
===========================================================================

constantinople.go - the Constantinople fortress switch (port-only).

Port-only, not native. SRO_FORT_CONSTANTINOPLE is off by default. The
importer (scripts/constantinople_import.py) stages the fortress's 36
regions, its 49 event zones and its siegefortress row into the variant
build while the switch is on; the server's fortress line (the authority
catalog, appendFortressStructures) is data-driven, so this file owns
only the switch itself: enterworld/fortressdata.go asks it before the
Constantinople row joins the client-facing fortress list, and a build
with the switch off contains none of the data at all.

===========================================================================
*/
package monster

import (
	"os"
	"strings"
)

// EnvConstantinople is the shard switch (shardsettings, nomad var
// fort_constantinople). Off is the native v1.150 shard.
const EnvConstantinople = "SRO_FORT_CONSTANTINOPLE"

/*
================
ConstantinopleEnabled
================
*/
func ConstantinopleEnabled() bool { return switchOn(EnvConstantinople) }

/*
================
switchOn

The fortress switches' reading: "1", "on" or "true" in any case; any
other value, or none, is the native off.
================
*/
func switchOn(env string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(env))) {
	case "1", "on", "true":
		return true
	}
	return false
}
