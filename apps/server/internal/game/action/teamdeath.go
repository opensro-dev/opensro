/*
===========================================================================

teamdeath.go - whether a team death revives where it fell

Natively a team death (pk.DeathTeam: free-battle capes, the arena) revives
at the present point with 40 percent HP and MP at any level, whatever the
choice (the reference server's rebirth request), and its notice labels
the death box "standing" (0x37CE, client 52FF33). The owner asked for that as a
server setting defaulting to native (2026-10-13). SRO_TEAM_DEATH_REVIVE
unset or "on" is native; "off" is port-only, not native: a team death
follows the town rule (the present point only at level 10 or below) and
its notice carries no team kind, so the box reads "point".

===========================================================================
*/
package action

import (
	"fmt"
	"os"
	"strings"

	"opensro.online/server/internal/game/pk"
)

// EnvTeamDeathRevive names the setting: unset or "on" native, "off" the
// town rule.
const EnvTeamDeathRevive = "SRO_TEAM_DEATH_REVIVE"

/*
================
ParseTeamDeathRevive

Whether a team death revives in place: "" and "on" are native (true),
"off" the town rule. A malformed value is an error, so a typo never ships
the wrong rule silently.
================
*/
func ParseTeamDeathRevive(text string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "", "on", "1", "true":
		return true, nil
	case "off", "0", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s: %q is not on or off", EnvTeamDeathRevive, text)
}

/*
================
TeamDeathReviveFromEnv
================
*/
func TeamDeathReviveFromEnv() (bool, error) {
	return ParseTeamDeathRevive(os.Getenv(EnvTeamDeathRevive))
}

/*
================
revivesInPlace

Whether this death kind revives where it fell: a team death, unless the
shard set the town rule.
================
*/
func (rt *Runtime) revivesInPlace(kind uint8) bool {
	return kind == uint8(pk.DeathTeam) && !rt.TeamDeathTownRule
}

/*
================
deathNoticeKind

The kind the 0x37CE notice carries. Port-only, not native: under the town
rule a team death reports no kind, so the death box does not promise a
revival it will not get.
================
*/
func (rt *Runtime) deathNoticeKind(kind uint8) uint8 {
	if kind == uint8(pk.DeathTeam) && rt.TeamDeathTownRule {
		return uint8(pk.DeathNone)
	}
	return kind
}
