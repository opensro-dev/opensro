/*
===========================================================================

jobsuitarmor.go - trader and hunter suits hide worn armour

Port-only, not native (owner decision 2026-10-14, for the balance advisor).
Natively only a thief suit hides the armour it is worn over: the client's
The client's CCObjCharacter_CheckJobVisualMask (8E8010) drops a worn piece of priority
above the suit's 0x32 (client ItemTID_GetVisualPriority 8E8710: armour 0x96) when
its mask overlaps the suit's, and the thief suits' mask is 0x73F (armour
sockets 0-5) while the trader's and hunter's is 0x700. With
SRO_JOB_SUIT_HIDES_ARMOR on, the GameWorld publishes the rule at world
entry and the client gives every job suit the armour bits, as a thief
suit has them (equipment-visuals.ts). The server keeps no appearance; the
rule is presentation only. Off, the default here, is native.

===========================================================================
*/
package action

import (
	"os"
	"strings"
)

// EnvJobSuitHidesArmor turns the rule on ("on", "1", "true").
const EnvJobSuitHidesArmor = "SRO_JOB_SUIT_HIDES_ARMOR"

/*
================
JobSuitHidesArmorFromEnv
================
*/
func JobSuitHidesArmorFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobSuitHidesArmor))) {
	case "on", "1", "true":
		return true
	}
	return false
}
