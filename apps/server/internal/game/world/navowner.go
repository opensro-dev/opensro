package world

// Surface ownership value types: WHICH walkable surface a position stands on.
// They live in this leaf package so every mover (players in simulation,
// monsters in monster) carries the same identity. The contract and native
// evidence are documented in simulation/navowner.go and movement/navowner.go.

// NavOwnerKind classifies a NavOwner.
type NavOwnerKind uint8

const (
	// NavOwnerUnresolved: no retained cell. Consumers resolve with the native
	// FindNavCell rule (teleport semantics) before walking.
	NavOwnerUnresolved NavOwnerKind = iota
	// NavOwnerTerrain: the terrain heightfield owns the position.
	NavOwnerTerrain
	// NavOwnerObject: one triangle cell of a placed object nav mesh owns it.
	NavOwnerObject
	// NavOwnerDungeon: one cell of a dungeon block's nav mesh owns it.
	// Object names it: Surface is the dungeon region, Object the block's
	// DOF ordinal, Mesh and Cell the cell (#1727).
	NavOwnerDungeon
)

// NavObjectCell addresses one object nav cell the way the movement authority
// indexes placements: the surface bundle that resolved it (its seed region),
// the anchor sector offset from that seed, the placement index within that
// anchor's set, the mesh index within the placement and the triangle cell.
// It is a value identity (no pointers) so state can copy and compare it.
type NavObjectCell struct {
	Surface uint16
	DX, DZ  int8
	Object  int32
	Mesh    int32
	Cell    int32
}

// NavOwner is the retained native pNavCell of a position (the tagNavPos at
// the character object's +0x7C in the reference server).
type NavOwner struct {
	Kind   NavOwnerKind
	Object NavObjectCell // valid only for NavOwnerObject
}

// Resolved reports whether the owner names a surface.
func (o NavOwner) Resolved() bool { return o.Kind != NavOwnerUnresolved }

// TerrainOwner is the terrain surface owner.
func TerrainOwner() NavOwner { return NavOwner{Kind: NavOwnerTerrain} }

// NavOwnerSpan is the owner over one fraction range [From, To] of a travel
// chord. Outside every span of a walk the chord is unresolved.
type NavOwnerSpan struct {
	From, To float64
	Owner    NavOwner
}

// OwnerAtFraction picks the span covering t (inclusive boundaries; the later
// span wins at a shared boundary, where the walker already stands in the next
// cell).
func OwnerAtFraction(spans []NavOwnerSpan, t float64) NavOwner {
	const eps = 1e-9
	owner := NavOwner{}
	for _, span := range spans {
		if t >= span.From-eps && t <= span.To+eps {
			owner = span.Owner
		}
	}
	return owner
}
