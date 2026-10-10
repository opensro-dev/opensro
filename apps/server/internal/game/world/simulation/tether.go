/*
===========================================================================

tether.go - movement tethers: how far a mover may walk from an anchor

A trader whose trade transport stands parked may not walk away from it.
The native server checks every movement step of the player
(CGObjPC_ValidateMoveAgainstCOSTethers 4F1230, CGObjPC vtable +0x620,
through CCOSManager_ValidateOwnerMovementTethers 4FD1D0) and stops the
player when the step would take it further from the transport while it
already stands past the range. The action owner knows where the transport
stands; it replaces the whole tether set here once per tick, and the ground
walk consults it on each finite step under the store's own lock.

===========================================================================
*/
package simulation

import (
	"math"

	worldgeom "opensro.online/server/internal/game/world"
)

// TradeTransportTetherRange is 4FD1D0's 1000.0 (0xB45C68): the planar
// distance a trader may stand from a parked trade transport before steps
// that lead further away are refused. The client prints it as 100 m.
const TradeTransportTetherRange = 1000.0

// TetherTradeTransport is 4FD1D0's reason 1 (the trade transport); reason 2,
// the 300-unit capture-quest monster, has no tether here because a captured
// monster follows its owner (petai.go).
const TetherTradeTransport uint8 = 1

/*
================
Tether

One mover's anchor and range, with the native reason byte its refusal
reports.
================
*/
type Tether struct {
	Anchor Spawn
	Range  float64
	Reason uint8
}

/*
================
tetherDistance

4FD1D0 measures Pos_Relative3D with a zero Y: the planar distance, its
squared sum and root stored as float32.
================
*/
func tetherDistance(from, anchor Spawn) float32 {
	a := worldgeom.RegionXZ{RegionID: anchor.RegionID, X: anchor.X, Z: anchor.Z}
	b := worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}
	dx, dz := worldgeom.Delta(a, b)
	squared := float32(float64(float32(dx))*float64(float32(dx)) + float64(float32(dz))*float64(float32(dz)))
	return float32(math.Sqrt(float64(squared)))
}

/*
================
Tether.Refuses

4FD1D0: a step is refused only when the mover already stands past the
range and the step's end lies further from the anchor than its start. A
mover inside the range may take the step that carries it past (the native
one-step overshoot), and one past it may always walk back.
================
*/
func (t Tether) Refuses(from, to Spawn) bool {
	before := tetherDistance(from, t.Anchor)
	if !(before > float32(t.Range)) {
		return false
	}
	return tetherDistance(to, t.Anchor) > before
}

/*
================
ReplaceTethers

The action owner's whole tether set for this tick, keyed by WorldKey. A
mover missing from the set has no tether, so an unsummoned, mounted or dead
transport releases its trader on the next tick without a separate clear.
================
*/
func (st *WorldStore) ReplaceTethers(tethers map[string]Tether) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(tethers) == 0 {
		st.tethers = nil
		return
	}
	st.tethers = tethers
}

/*
================
TetherOf

The tether the ground walk holds the mover to, if any.
================
*/
func (st *WorldStore) TetherOf(key string) (Tether, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	tether, ok := st.tethers[key]
	return tether, ok
}
