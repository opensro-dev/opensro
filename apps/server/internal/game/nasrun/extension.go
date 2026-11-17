/*
===========================================================================

extension.go - the extension gear's span and ceiling

Port-only, not native to v1.150 (#3456 slice 4). The v1.188 server's gear
(3/3/13/16) extends a woken item's clock by its own Param1, four weeks on
the VSRO rows, with no ceiling. The owner's pick (2026-11-16, #3456) is
the upkeep rate instead: each gear adds seven days, and a clock never
runs more than 28 days ahead. A gear that would pass the ceiling is
refused and kept (inferred: the pick says "stacking up to 28 days", and
spending a gear for nothing would be a loss the player cannot see).

===========================================================================
*/
package nasrun

const (
	// ExtensionSpanSeconds is what one gear adds: seven days.
	ExtensionSpanSeconds = 7 * 24 * 60 * 60

	// ExtensionCeilingSeconds is the most a clock may run ahead of now.
	ExtensionCeilingSeconds = 28 * 24 * 60 * 60
)

/*
================
ExtendWithin

The data word one gear leaves (Extend's rule), or ok false when the
Spirit is dormant or the new expiry would lie more than
ExtensionCeilingSeconds past now.
================
*/
func ExtendWithin(data uint32, nowUnix int64) (next uint32, ok bool) {
	next, ok = Extend(data, ExtensionSpanSeconds, nowUnix)
	if !ok || int64(next)-nowUnix > ExtensionCeilingSeconds {
		return data, false
	}
	return next, true
}
