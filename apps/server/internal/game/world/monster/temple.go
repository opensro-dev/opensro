/*
===========================================================================

temple.go - the temple band population switch (the consumer joins here).

Port-only, not native. SRO_CAP110_TEMPLE is off by default and requires
SRO_CAP110_MONSTERS: the import writes cap110-temple.txt only on that
pairing (scripts/cap110_monsters.py), and AppendTemple - the population
consumer, #1800 step 3 slice 2 - refuses the same pairing at load, in
this file. Off never reads the temple file; the native game has no
temple, so off is the native state.

===========================================================================
*/
package monster

import (
	"os"
	"strings"
)

const EnvCap110Temple = "SRO_CAP110_TEMPLE"
const templePopulationFile = "cap110-temple.txt"

/*
================
TempleEnabled
================
*/
func TempleEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvCap110Temple))) {
	case "1", "on", "true":
		return true
	}
	return false
}
