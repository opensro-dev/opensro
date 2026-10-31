/*
===========================================================================

drain.go - the murderer status HP drain's numbers

A living player in murderer status (a positive PK penalty) loses a share of
its maximum HP on a fixed period. The action runtime owns the clock, the
commit and the publication; this file owns only the rate and the period, so
the rule's arithmetic has one home beside the other PK tables.

INFERENCE: the owner played the original and reports 5% of maximum HP every
10 s. The server binary is not on this machine, so the timing is an owner
estimate, not a read constant. The v1.150 vitals causes carry no bit for it
(simulation/gameready.go), so the action runtime publishes the loss as a
plain vitals refresh with source zero.

===========================================================================
*/

package pk

const (
	// MurdererDrainPercent is the share of max HP one drain pulse takes.
	MurdererDrainPercent int64 = 5
	// MurdererDrainPeriodMs is the drain period: 10 s between pulses
	// (owner estimate, see the file banner).
	MurdererDrainPeriodMs int64 = 10000
)

/*
================
DrainAmount

The HP one pulse takes: MurdererDrainPercent of the maximum, truncated.
The floor of one is the port's inference: a maximum below 20 would
otherwise lose nothing, and the rule's point is that the drain can kill.
================
*/
func DrainAmount(maximum int64) int64 {
	if maximum <= 0 {
		return 0
	}
	return max(maximum*MurdererDrainPercent/100, 1)
}
