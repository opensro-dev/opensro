/*
===========================================================================

jobtransport.go - job trade transports move thirty percent faster

Port-only, not native (#2474, owner request 2026-11-04). A trade
transport (a COS band-2 row a trader or thief summons for a trade run)
walks and runs at its characterdata speed natively - the speed the live
job transports run at today. With SRO_JOB_TRANSPORT_SPEED on, those
references walk and run 1.3 times as fast. The scale lands on the
loaded reference rows, so every owner that reads a COS's speed (summon,
entry spawn, pet AI, formation, combat chase) and the speeds published
to clients agree. The Item Mall's three transport families keep their
own 1.5 rule (malltransport.go) and are not scaled twice. Off is native;
the GameWorld's Nomad job turns it on.

===========================================================================
*/
package enterworld

import (
	"fmt"
	"os"
	"strings"
)

// EnvJobTransportSpeed turns the faster job transports on ("on", "1", "true").
const EnvJobTransportSpeed = "SRO_JOB_TRANSPORT_SPEED"

// jobTransportSpeedup multiplies a job transport's walk and run speeds.
const jobTransportSpeedup = 1.3

/*
================
JobTransportSpeedFromEnv
================
*/
func JobTransportSpeedFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJobTransportSpeed))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
isJobTransportRef

A trade transport reference (COS band 2) the mall rule does not already
own: the job system's transports.
================
*/
func isJobTransportRef(ref *CharacterRef) bool {
	return ref != nil && ref.TidWord&tidKindMask == tidKindCOS && ref.TidWord>>11 == transportBand && !isMallTransport(ref)
}

/*
================
ApplyJobTransportSpeed

Scales every job transport reference's walk and run speeds, before any
owner reads them; the count is the rows changed.
================
*/
func (t *TextdataItems) ApplyJobTransportSpeed() (int, error) {
	t.once.Do(t.load)
	if t.archive != nil {
		return 0, fmt.Errorf("%s: references are already archived", EnvJobTransportSpeed)
	}
	t.ownRows()
	scaled := 0
	for _, ref := range t.charactersByCodename {
		if isJobTransportRef(ref) {
			ref.WalkSpeed *= jobTransportSpeedup
			ref.RunSpeed *= jobTransportSpeedup
			scaled++
		}
	}
	// The summonable list holds copies of the same rows.
	for i := range t.summonableCharacters {
		if ref := &t.summonableCharacters[i]; isJobTransportRef(ref) {
			ref.WalkSpeed *= jobTransportSpeedup
			ref.RunSpeed *= jobTransportSpeedup
		}
	}
	return scaled, nil
}
