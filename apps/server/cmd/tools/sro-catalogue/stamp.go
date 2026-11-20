/*
===========================================================================

stamp.go - the catalogue's generatedAt, reproducible from the freeze

The host refuses to republish a release whose catalogue directory is
already installed with other bytes (opensro-ops catalogue.require_staged).
A wall-clock stamp made every rebuild of one freeze differ in that one
field, so a freeze could never be published again after a revert (r203n,
2026-11-20: 18,847 files equal but generatedAt). The release passes the
freeze's commit time as SOURCE_DATE_EPOCH (the reproducible-builds
convention); a hand run without it keeps the clock.

===========================================================================
*/

package main

import (
	"fmt"
	"strconv"
	"time"
)

// sourceDateEpoch is the reproducible-builds variable naming a build's time.
const sourceDateEpoch = "SOURCE_DATE_EPOCH"

/*
================
generatedAt

The header's time: SOURCE_DATE_EPOCH when set, else now. A malformed
value is refused, never replaced by the clock, so a release cannot ship
an unreproducible stamp by typo.
================
*/
func generatedAt(getenv func(string) string, now func() time.Time) (string, error) {
	value := getenv(sourceDateEpoch)
	if value == "" {
		return now().UTC().Format(time.RFC3339), nil
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 {
		return "", fmt.Errorf("%s must be non-negative unix seconds, got %q", sourceDateEpoch, value)
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339), nil
}
