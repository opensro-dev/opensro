/*
===========================================================================

skillarea_directional.go - area shapes 3 and 4: victims along a line

The reference server's directional range (shape 3) and
directional target (shape 4) selections gather everything
within 300 units and keep what the directional shape test
accepts: within reach along a direction vector and within the efr width of
its line. Shape 3 measures from the caster, along the unit vector to the
primary scaled by the action range; shape 4 measures from the caster along
the whole caster-to-primary vector, gathering around the primary.

Monsters and attackable players are selected alike (areacandidates.go);
the second pass over non-character objects (select bit 0x10) has
nothing to find.

===========================================================================
*/

package action

import (
	"math"
	"sort"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// directionalSearchRadius is the fixed gather around the search centre
// (the native radius collection, 300.0f for both shapes).
const directionalSearchRadius = 300

/*
================
vec3

Stored native vector components. Arithmetic widens only between explicit
float stores so directional skills and pursuit share the x87 contract.
================
*/
type vec3 struct{ x, y, z float32 }

/*
================
relative

The native relative position: to minus from across regions.
Callers admit compatible planes before requesting a direction.
================
*/
func relative(from, to simulation.Spawn) vec3 {
	dx, dz := worldgeom.Delta(
		worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z},
		worldgeom.RegionXZ{RegionID: to.RegionID, X: to.X, Z: to.Z},
	)
	return vec3{float32(dx), float32(to.Y - from.Y), float32(dz)}
}

/*
================
vec3.length

Natively the squared sum is stored before the square root rounds its return value.
================
*/
func (v vec3) length() float32 {
	x, y, z := float64(v.x), float64(v.y), float64(v.z)
	return float32(math.Sqrt(float64(float32(x*x + y*y + z*z))))
}

/*
================
vec3.normalized

Natively the reciprocal length is stored before multiplying each component.
Independent component division differs by a float32 ULP and can change
admission at an exact directional boundary.
================
*/
func (v vec3) normalized() vec3 {
	n := v.length()
	if n == 0 {
		return vec3{}
	}
	inverse := float32(1 / float64(n))
	return vec3{v.x * inverse, v.y * inverse, v.z * inverse}
}

/*
==================
inDirectionalShape

The native directional shape test for one candidate. rel is caster to candidate with its height
dropped, dir the reach vector. The candidate must lie within the caster's
radius plus the reach plus its own radius, and its distance from the line
(sine of the angle times the distance) must stay below its radius plus
the efr width. Nothing tests that it lies in front of the caster.
==================
*/
func inDirectionalShape(rel, dir vec3, casterRadius, candidateRadius int32, width uint32) bool {
	rel.y = 0
	distance := rel.length()
	reach := float64(casterRadius) + float64(dir.length())
	if float64(candidateRadius)+reach < float64(distance) {
		return false
	}
	d, r := dir.normalized(), rel.normalized()
	cosine := float32(float64(d.x)*float64(r.x) + float64(d.y)*float64(r.y) + float64(d.z)*float64(r.z))
	cosine = max(-1, min(1, cosine))
	// Natively acos and sin are both stored before scaling by distance.
	angle := float32(math.Acos(float64(cosine)))
	sine := float32(math.Sin(float64(angle)))
	lateral := float32(float64(sine) * float64(distance))
	return float64(lateral) < float64(uint32(candidateRadius)+width)
}

/*
==================
directionalVictims

The primary first, then every living candidate the shape accepts, in the
port's GID order, up to MaxTargets. reach is the action's base range
(RefSkill +0x92, else the attack-range param).
==================
*/
func (rt *Runtime) directionalVictims(q areaQuery, caster simulation.Spawn, casterRadius float64, primary combatTarget, area areaShape, reach float32) []combatTarget {
	out := []combatTarget{primary}
	if area.maxTargets <= 1 {
		return out
	}

	toPrimary := relative(caster, primary.at)
	var dir vec3
	q.center = primary.at
	if area.shape == 3 {
		// Natively the height is dropped before the direction is normalised.
		toPrimary.y = 0
		unit := toPrimary.normalized()
		dir = vec3{unit.x * reach, unit.y * reach, unit.z * reach}
		q.center = caster
	} else {
		dir = toPrimary
	}
	q.reach, q.nearest = directionalSearchRadius, true

	candidates := rt.areaCandidates(q)
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].target.gid < candidates[j].target.gid })
	for _, candidate := range candidates {
		if candidate.target.gid == primary.gid {
			continue
		}
		if !inDirectionalShape(relative(caster, candidate.target.at), dir, int32(casterRadius), int32(candidate.radius), area.width) {
			continue
		}
		out = append(out, candidate.target)
		if len(out) == int(area.maxTargets) {
			break
		}
	}
	return out
}

/*
================
areaShape

The part of efr kind 1 consumed by the directional selectors.
================
*/
type areaShape struct {
	shape      uint8
	width      uint32 // efr +8
	maxTargets uint8  // efr +0xC
}
