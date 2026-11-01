/*
===========================================================================

drain.go - the murderer status HP drain's number

The share of maximum HP a murderer's recovery pulse takes outside a safe
region. The action runtime owns the pulse, the gates and the publication;
the rule's arithmetic has one home beside the other PK tables.

===========================================================================
*/

package pk

// murdererDrainRate is the native multiplier: a single-precision 0.02 held in
// a double, so 2% of a whole fifty truncates one point low (100 loses 1).
const murdererDrainRate = float64(float32(0.02))

/*
================
DrainAmount

The HP one pulse takes, truncated as the native conversion does. There is no
floor: a maximum of 50 or less loses nothing.
================
*/
func DrainAmount(maximum int64) int64 {
	if maximum <= 0 {
		return 0
	}
	return int64(float64(maximum) * murdererDrainRate)
}
