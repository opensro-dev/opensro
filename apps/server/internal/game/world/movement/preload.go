/*
===========================================================================

preload.go - resolve immutable outdoor navigation before the world ticks

Monster placement and path queries share these caches with player movement.
Loading a first-use region inside the simulation stalls every session. Boot
performs the same resolutions before readiness, without changing query results,
population order, navigation admission or the runtime failure policy.

===========================================================================
*/
package movement

import (
	"fmt"
	"slices"
)

/*
================
PreloadOutdoorNavigation

Called before publishing the validator to gameplay. Regions and placement
offsets are sorted so startup reads are reproducible. Shared bundles load once
through the existing caches. Missing optional assets retain their cached nil.
================
*/
func (v *WaterValidator) PreloadOutdoorNavigation() (int, error) {
	catalog := v.loadCatalog()
	if catalog == nil {
		return 0, fmt.Errorf("movement: cannot preload absent navigation catalog")
	}
	regions := make([]uint16, 0, len(catalog.RegionsByID))
	for name := range catalog.RegionsByID {
		if region := parseRegionID(name); region != 0 {
			regions = append(regions, region)
		}
	}
	slices.Sort(regions)
	regions = slices.Compact(regions)
	seen := make(map[*groundSurface]bool)
	lookups := v.validatorPlanes()
	count := 0
	for _, region := range regions {
		if lookups.preloadRegion(region, seen) {
			count++
		}
	}
	return count, nil
}
