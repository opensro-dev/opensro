/*
===========================================================================

publication.go - canonical division identity for bootstrap publication

Snapshot construction and its outer publication gate must resolve aliases
and the default division identically.

===========================================================================
*/
package enterworld

/*
================
resolveBootstrapDivision
================
*/
func resolveBootstrapDivision(deps *Deps, requested string) string {
	if deps.ResolveDivisionID != nil {
		return deps.ResolveDivisionID(requested)
	}
	if requested == "" {
		return DefaultDivisionID
	}
	return requested
}
