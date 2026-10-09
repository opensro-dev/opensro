/*
===========================================================================

stalldecoration.go - which booth a character's stall shows

A stall's spawn and open rows carry one u32 decoration (client 86A26D,
CICharactor_SetStallState 86A880 loads that item's model, or the
country's default stall for zero). A running premium-package booth (the
composite BFI1 work, CBuffItem) shows while it lasts; otherwise the
permanent decoration the character applied, if any.

===========================================================================
*/
package domain

/*
================
StallDecorationAt

The decoration item reference a stall opened at nowMs shows, or zero.
================
*/
func (c *Character) StallDecorationAt(nowMs int64) uint32 {
	if c == nil {
		return 0
	}
	for _, job := range c.CompositeJobs {
		if job.Kind == CompositeBuffItem && job.Target != 0 && job.EndUnixMs > nowMs {
			return job.Target
		}
	}
	return c.StallDecoration
}
