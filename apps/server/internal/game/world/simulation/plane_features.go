/*
===========================================================================

plane_features.go - which world features each plane has (#1646)

Some features exist only in the open world. Rather than test the dungeon
region bit at each feature's door, every feature's availability per plane
is one row here, read by name (featuresAt(region).CaravanBandits), so the
table is the single place the outdoor/cave difference lives and a feature
a cave later gains is a one-field change. The cave column is today's
behaviour; where the reason is the port's reading rather than native
evidence, the field says so (the AGENTS.md scope rule).

===========================================================================
*/
package simulation

/*
================
PlaneFeatures
================
*/
type PlaneFeatures struct {
	// QuestMonsterSpawns: a quest's summoned guardian or lure. Inference:
	// the three quest guardians appear at field positions, and the port
	// places them with the outdoor nest placement (radius sample, then
	// the sector-grid region clamp), which has no cave-floor form.
	QuestMonsterSpawns bool
	// CaravanBandits: the ambush around a trade vehicle. Inference: the
	// trade routes run over open terrain and a caravan never enters a cave,
	// and the bandits are placed around the vehicle by the same outdoor
	// nest placement.
	CaravanBandits bool
	// UniqueReturnAnchors: a unique's field nest as a reverse return
	// scroll destination (port-only, action/reversemap.go). Field nests
	// only: a return point must be one the scroll can land on in the open
	// world.
	UniqueReturnAnchors bool
	// Dormancy: an empty region's settled population sleeps (port-only,
	// monster_dormancy.go). Inference: a cave floor is one region id for
	// the whole floor, so region emptiness says nothing about the part of
	// the floor a player stands in; caves stay awake.
	Dormancy bool
}

/*
================
featuresFor

The features of a plane of this kind: every one outdoors, none of these
in a cave.
================
*/
func featuresFor(kind PlaneKind) PlaneFeatures {
	if kind == PlaneCave {
		return PlaneFeatures{}
	}
	return PlaneFeatures{QuestMonsterSpawns: true, CaravanBandits: true, UniqueReturnAnchors: true, Dormancy: true}
}

/*
================
featuresAt

The features of the plane regionID belongs to.
================
*/
func featuresAt(regionID uint16) PlaneFeatures {
	return featuresFor(PlaneKindOf(regionID))
}
