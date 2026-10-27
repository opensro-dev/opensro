/*
===========================================================================

stalldecoration.go - which booth a character's stall shows

A stall's spawn and open rows carry one u32 decoration (client 86A26D,
CICharactor_SetStallState 86A880 loads that item's model, or the country's
default stall for zero). The permanent decoration the character applied with
an ITEM_MALL_BOOTH_* item wins; a running premium-package booth (the
composite BFI1 work, a buff item) shows only while no permanent decoration
is applied. Natively the server's stall decoration lookup returns the
per-session override at pc+0x21D4 when set (written by a 0x7160 activation,
cleared when the stall closes) and the persistent decoration at pc+0x1F20
otherwise; the port's per-session state is the stall's Decoration snapshot
taken when the stall opens, and the premium booth is applied at that same
moment instead of through 0x7160, so the passive composite job must never
shadow the purchased decoration.

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
	if c.StallDecoration != 0 {
		return c.StallDecoration
	}
	for _, job := range c.CompositeJobs {
		if job.Kind == CompositeBuffItem && job.Target != 0 && job.EndUnixMs > nowMs {
			return job.Target
		}
	}
	return 0
}
