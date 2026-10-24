/*
===========================================================================

growth_config.go - parsing shared by beta growth and deployment admission

The gameplay owner keeps the numeric bounds. Runtime overrides retain their
legacy fallback, while deployment can refuse an invalid value before mutation.
These beta settings are port-only, not native.

===========================================================================
*/
package progression

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

/*
================
ParseBetaGrowthSetting

Parses one explicit numeric override without reading process state. Empty
values are invalid here; callers own their runtime or deployment defaults.
================
*/
func ParseBetaGrowthSetting(env, raw string) (int64, error) {
	minimum := int64(1)
	maximum := int64(math.MaxInt64)
	switch env {
	case EnvBetaSkillExpRate:
	case EnvBetaDropRate:
		maximum = maxBetaDropRate
	case EnvBetaDropCap:
		minimum = 0
		maximum = maxBetaDropCap
	case EnvBetaGoldRate:
		maximum = maxBetaGoldRate
	case EnvBetaRareRate:
		maximum = maxBetaRareRate
	default:
		return 0, fmt.Errorf("%s=%q is not a beta growth rate setting", env, raw)
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s=%q must be an integer in %d..%d", env, raw, minimum, maximum)
	}
	return value, nil
}
