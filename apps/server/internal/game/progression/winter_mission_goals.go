/*
===========================================================================

winter_mission_goals.go - fixed Server Mission goals for testing

Port-only, not native (SRO_WINTER_MISSION_GOALS, unset by default). The
native goals follow the players online (winter_missions.go); one tester
alone would hunt 460 Snow Slaves for one summon. The switch names a fixed
goal per mission, "30001=3,30002=5,30004=2", and a mission it names takes
that goal instead of the native one, a kept row's included. Unset, or a
value that does not parse, is the native goals; the composition root says
which at boot.

===========================================================================
*/
package progression

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvWinterMissionGoals names the fixed test goals.
const EnvWinterMissionGoals = "SRO_WINTER_MISSION_GOALS"

/*
================
WinterMissionGoals

A fixed goal per mission identity; nil is the native goals.
================
*/
type WinterMissionGoals map[uint32]int

/*
================
WinterMissionGoalsFromEnv

The switch's goals. An unset switch is nil and no error; a malformed one
is nil and the error the boot log names.
================
*/
func WinterMissionGoalsFromEnv() (WinterMissionGoals, error) {
	raw := strings.TrimSpace(os.Getenv(EnvWinterMissionGoals))
	if raw == "" {
		return nil, nil
	}
	return ParseWinterMissionGoals(raw)
}

/*
================
ParseWinterMissionGoals

"ID=GOAL" pairs, comma-separated: each ID a winter mission, each goal a
positive count.
================
*/
func ParseWinterMissionGoals(raw string) (WinterMissionGoals, error) {
	goals := WinterMissionGoals{}
	for _, pair := range strings.Split(raw, ",") {
		id, goal, found := strings.Cut(strings.TrimSpace(pair), "=")
		mission, idErr := strconv.ParseUint(strings.TrimSpace(id), 10, 32)
		count, goalErr := strconv.Atoi(strings.TrimSpace(goal))
		if !found || idErr != nil || goalErr != nil || count < 1 {
			return nil, fmt.Errorf("%s: %q is not MISSION=GOAL with a positive goal", EnvWinterMissionGoals, pair)
		}
		if _, known := findWinterMission(uint32(mission)); !known {
			return nil, fmt.Errorf("%s: %d is not a winter mission (30001-30004)", EnvWinterMissionGoals, mission)
		}
		goals[uint32(mission)] = count
	}
	return goals, nil
}
