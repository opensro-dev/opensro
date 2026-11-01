/*
===========================================================================

banditcap.go - the shard's cap on bandits per ambush (port-only, not native)

The server's caravan ambush spawns as many bandits as the cargo's star
rating gives (its bandit spawn count): one per 306 of
cargo value, one more on half the draws, doubled on 2%, with no ceiling.
A full load at high level draws dozens. The owner approved a cap as a
server setting (2026-10-13, 16; 2026-10-14, 8). The same function spawns
the thieves sent at a trader and the hunters sent at a thief (TacticsID),
so the cap holds for both.

SRO_CARAVAN_BANDIT_CAP is a number 1..maxBanditCap, or unset/"off"/"0"
(native, uncapped). The cap trims the native count after it is rolled; the
bandits that remain draw exactly as natively (action/caravan.go).

===========================================================================
*/
package caravan

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EnvBanditCap names the setting: a number, or "off" for native.
const EnvBanditCap = "SRO_CARAVAN_BANDIT_CAP"

// maxBanditCap bounds the setting; a native count never needs more.
const maxBanditCap = 1000

/*
================
ParseBanditCap

"", "off" and "0" are native (0, uncapped); anything else must be a count
in 1..maxBanditCap. A malformed value is an error, so a typo never ships
the wrong cap silently.
================
*/
func ParseBanditCap(text string) (int, error) {
	text = strings.TrimSpace(text)
	switch strings.ToLower(text) {
	case "", "off", "0":
		return 0, nil
	}
	n, err := strconv.Atoi(text)
	if err != nil || n < 1 || n > maxBanditCap {
		return 0, fmt.Errorf("%s: %q is not a count between 1 and %d", EnvBanditCap, text, maxBanditCap)
	}
	return n, nil
}

/*
================
BanditCapFromEnv
================
*/
func BanditCapFromEnv() (int, error) {
	return ParseBanditCap(os.Getenv(EnvBanditCap))
}

/*
================
CappedSpawnCount

The native count trimmed to the shard's cap; a cap of 0 is native.
================
*/
func CappedSpawnCount(count, limit int) int {
	if limit > 0 && count > limit {
		return limit
	}
	return count
}
