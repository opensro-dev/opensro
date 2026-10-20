/*
===========================================================================

temple_retained_owner_test.go - a retained south-sector owner still crosses

Report BR-261018-1104-A69F (#1213): the walker stepped onto the Fertility
Temple stair (asset 1300) in region 0x6587 and kept that sector's owner,
whose bundle copy of the stair carries the placeholder link table
(target 0xffff). Native QueryMovement (98B300) steps the CURRENT sector's
CRTNavMeshTerrain, whose inst list in 0x6687 authors the stair edge 93 ->
deck edge 408 portal, so the retained owner must be rebound to the same
placement in the current region's bundle before the walk reads its links.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestFertilityTempleRetainedSouthOwnerCrosses

The incident lane itself: ClipMovementPathFrom from the server's req826
rest position on stair cell 92, holding the south sector's retained
owner, to the deck cell the portal receives into. At base the walk reads
the placeholder links and blocks on cell 92; rebound, the authored link
transfers it onto the deck and it arrives.
================
*/
func TestFertilityTempleRetainedSouthOwnerCrosses(t *testing.T) {
	v := realAuthorityValidator(t)
	const region = 0x6687
	set := v.objectNavSetForOffset(v.surfaceForRegion(region), 0, 0)
	bridge := set[0]
	link := bridge.placement.links[0]
	deck := set[link.target]
	to := templePortalStand(region, deck, link.targetEdge)
	wantCell := int(deck.meshes[0].outline.srcCell[link.targetEdge])

	// The owner minted in the south sector: the same stair placement in
	// region 0x6587's bundle (its link table is the 0xffff placeholder).
	south := v.objectNavSetForOffset(v.surfaceForRegion(0x6587), 0, 0)
	southIndex := -1
	for i := range south {
		if south[i].placement.assetID == templeBridgeAssetID {
			southIndex = i
			break
		}
	}
	if southIndex < 0 {
		t.Fatal("south bundle carries no temple stair")
	}
	owner := simulation.NavOwner{Kind: simulation.NavOwnerObject, Object: simulation.NavObjectCell{
		Surface: 0x6587, Object: int32(southIndex), Mesh: 0, Cell: 92,
	}}

	// The server's req826 rest position on cell 92 (BR-261018-1104-A69F).
	from := simulation.Spawn{RegionID: region, X: 367.63, Y: -34.83, Z: 412.04}
	got := v.ClipMovementPathFrom(from, owner, to)
	if got.Outcome != ClipArrived {
		t.Fatalf("retained south owner from %+v: %s at %+v on %+v, want arrived on the deck", from, got.Outcome, got.Rest, got.RestOwner)
	}
	if got.RestOwner.Object.Surface != region || int(got.RestOwner.Object.Cell) != wantCell {
		t.Fatalf("crossing rests on %+v, want the deck cell %d in region %#x", got.RestOwner, wantCell, region)
	}
	if math.Abs(got.Rest.Y-to.Y) > 0.01 || math.Hypot(got.Rest.X-to.X, got.Rest.Z-to.Z) > 0.01 {
		t.Fatalf("crossing rests at %+v, want %+v", got.Rest, to)
	}
}
