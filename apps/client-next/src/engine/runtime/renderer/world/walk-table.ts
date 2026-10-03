/*
===========================================================================

walk-table.ts - the typed data the selection walk reads first

The world renderer's selection walk runs over every group of the current
scene whenever the view moves. A dragged camera leaves most groups outside
the frustum, and touching each group's objects only to learn that cost
more than the work itself. This table holds, per group in walk order, the
numbers that decide what the walk must do:

- terrain: a box around all its ranges, and how many it chose last pass;
- instanced: a sphere around every placement's frustum bound, the spread
  of the placement origins, the eye distances at which its objects change
  fade, and per placement slot the fade inputs (origin, radius, range,
  scenery flag) and fade row, laid out contiguously so the fade pass of a
  group outside the frustum reads only typed columns;
- the resident slots of each instanced group for the current target cell,
  and how many placements it showed last pass.

Bounds are grown so that rejecting them rejects every member test exactly
(instance-bounds.ts).

===========================================================================
*/
import type { WorldGroup } from "@/engine/contracts/scene";
import { instanceGroupSphere } from "@/engine/foundation/rendering/instance-bounds";

export const WALK_SKY = 0;
export const WALK_TERRAIN = 1;
export const WALK_INSTANCED = 2;
export const WALK_PLAIN = 3;

// Slot fade kinds: a fixed range, the scenery range (it follows the
// background distance), or no descriptor (always shown, never fades).
export const SLOT_FIXED = 0;
export const SLOT_SCENERY = 1;
export const SLOT_UNFADED = 2;

// Fade distances are float32 sums of world coordinates; a unit of headroom
// covers their rounding at any world distance.
const REACH_MARGIN = 1;

// A terrain box grown by this much rejects only when every range in it
// would: the frustum tests' float headroom is a few hundredths of a unit.
const BOX_MARGIN = 1;

export interface WalkTable {
	readonly groups: readonly WorldGroup[];
	readonly kind: Uint8Array;
	// Groups with object fades (instanced groups with descriptors).
	readonly faded: Uint8Array;
	// Terrain: min xyz, max xyz (6 a group).
	readonly box: Float64Array;
	// Ranges a terrain group chose in its last pass (-1 before one).
	readonly shown: Int32Array;
	// Instanced: centre xyz, frustum radius, origin spread (5 a group).
	readonly sphere: Float64Array;
	// Instanced fade reach: smallest and largest range + radius of fixed
	// range objects, smallest and largest radius of scenery objects.
	readonly reach: Float64Array;
	// Instanced placements a group showed in its last pass (-1 before one).
	readonly count: Int32Array;
	// The walk that last showed a group, and the triangles it showed then:
	// a walk that keeps the view reuses both.
	readonly seen: Float64Array;
	readonly triangles: Float64Array;
	// Per slot, at slotBase[group] + slot: origin xyz (3 a slot), fade
	// radius and range, SLOT_* kind, and fade row (-1 unresolved; the
	// renderer resolves a group's rows once each fade epoch).
	readonly slotBase: Int32Array;
	readonly origin: Float64Array;
	readonly radius: Float64Array;
	readonly range: Float64Array;
	readonly slotKind: Uint8Array;
	readonly row: Int32Array;
	// The fade epoch a group's rows were resolved for.
	readonly rowsEpoch: Float64Array;
	// Resident slots of a group at its slot base, how many, and the target
	// cell (x, z) they were found for.
	readonly resident: Uint32Array;
	readonly residentCount: Int32Array;
	readonly residentCell: Float64Array;
	// The walk's scratch: per resident slot (at the group's slot base), the
	// published alpha of its object this frame, or -1 when it is out.
	readonly alpha: Int16Array;
}

/*
================
compileWalkTable

pickBounds: placement bounds by vertex positions. Animated meshes and
meshes without bounds are tested as spheres.
================
*/
export function compileWalkTable(
	groups: readonly WorldGroup[],
	pickBounds: WeakMap<Float32Array, readonly number[]>
): WalkTable {
	const count = groups.length, slotBase = new Int32Array( count + 1 );
	for ( let i = 0; i < count; i++ ) {
		const group = groups[i]!;
		const instanced = !group.material.sky && !group.ranges && group.instanceRadius !== undefined;
		slotBase[i + 1] = slotBase[i]! + (instanced ? group.geometry.instances!.length / 16 : 0);
	}
	const slots = slotBase[count]!;
	const table: WalkTable = {
		groups,
		kind: new Uint8Array( count ),
		faded: new Uint8Array( count ),
		box: new Float64Array( count * 6 ),
		shown: new Int32Array( count ).fill( -1 ),
		sphere: new Float64Array( count * 5 ),
		reach: new Float64Array( count * 4 ),
		count: new Int32Array( count ).fill( -1 ),
		seen: new Float64Array( count ).fill( -1 ),
		triangles: new Float64Array( count ),
		slotBase,
		origin: new Float64Array( slots * 3 ),
		radius: new Float64Array( slots ),
		range: new Float64Array( slots ),
		slotKind: new Uint8Array( slots ),
		row: new Int32Array( slots ).fill( -1 ),
		rowsEpoch: new Float64Array( count ).fill( NaN ),
		resident: new Uint32Array( slots ),
		residentCount: new Int32Array( count ),
		residentCell: new Float64Array( count * 2 ).fill( NaN ),
		alpha: new Int16Array( slots )
	};
	for ( let i = 0; i < count; i++ ) {
		const group = groups[i]!;
		if ( group.material.sky ) table.kind[i] = WALK_SKY;
		else if ( group.ranges ) {
			table.kind[i] = WALK_TERRAIN;
			terrainBox( group, table.box, i * 6 );
		} else if ( group.instanceRadius !== undefined ) {
			table.kind[i] = WALK_INSTANCED;
			compileInstanced( table, i, group.geometry.bones ? undefined : pickBounds.get( group.geometry.positions ) );
		} else table.kind[i] = WALK_PLAIN;
	}
	return table;
}

/*
================
compileInstanced
================
*/
function compileInstanced( table: WalkTable, i: number, bounds: readonly number[] | undefined ) {
	const group = table.groups[i]!, instances = group.geometry.instances!, base = table.slotBase[i]!;
	const sphere = instanceGroupSphere( instances, bounds, group.instanceRadius! );
	table.sphere.set( sphere, i * 5 );
	let spread = 0;
	for ( let slot = 0; slot * 16 < instances.length; slot++ ) {
		const at = slot * 16, x = instances[at + 12]!, y = instances[at + 13]!, z = instances[at + 14]!;
		spread = Math.max( spread, Math.hypot( x - sphere[0]!, y - sphere[1]!, z - sphere[2]! ) );
		table.origin[(base + slot) * 3] = x;
		table.origin[(base + slot) * 3 + 1] = y;
		table.origin[(base + slot) * 3 + 2] = z;
		const descriptor = group.visibility?.[slot];
		if ( !descriptor ) {
			table.slotKind[base + slot] = SLOT_UNFADED;
			continue;
		}
		table.radius[base + slot] = descriptor.radius;
		table.range[base + slot] = descriptor.range;
		table.slotKind[base + slot] = descriptor.sceneryRange ? SLOT_SCENERY : SLOT_FIXED;
	}
	table.sphere[i * 5 + 4] = spread + REACH_MARGIN;
	table.faded[i] = group.visibility ? 1 : 0;
	let fixedLow = Infinity, fixedHigh = -Infinity, sceneryLow = Infinity, sceneryHigh = -Infinity;
	for ( const d of group.visibility ?? [] ) {
		if ( d.sceneryRange ) {
			sceneryLow = Math.min( sceneryLow, d.radius );
			sceneryHigh = Math.max( sceneryHigh, d.radius );
		} else {
			fixedLow = Math.min( fixedLow, d.range + d.radius );
			fixedHigh = Math.max( fixedHigh, d.range + d.radius );
		}
	}
	table.reach.set( [ fixedLow, fixedHigh, sceneryLow, sceneryHigh ], i * 4 );
}

/*
================
terrainBox
================
*/
function terrainBox( group: WorldGroup, box: Float64Array, at: number ) {
	box.set( [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ], at );
	for ( const range of group.ranges! ) {
		const b = range.bounds, c = range.center, r = range.radius;
		for ( let axis = 0; axis < 3; axis++ ) {
			const low = b ? b[axis]! : c[axis]! - r, high = b ? b[axis + 3]! : c[axis]! + r;
			box[at + axis] = Math.min( box[at + axis]!, low - BOX_MARGIN );
			box[at + axis + 3] = Math.max( box[at + axis + 3]!, high + BOX_MARGIN );
		}
	}
}

/*
================
residentSlots

The resident slots of instanced group i for a target cell: those whose
object is associated with a cell within its cell radius, and every slot
without a descriptor. Native membership depends on the target cell, not
on each sub-cell camera sample, so the list is rebuilt only when that cell
changes. Returns how many there are (at table.resident[slotBase[i]]).
================
*/
export function residentSlots( table: WalkTable, i: number, cellX: number, cellZ: number ): number {
	if ( table.residentCell[i * 2] === cellX && table.residentCell[i * 2 + 1] === cellZ ) {
		return table.residentCount[i]!;
	}
	const group = table.groups[i]!, base = table.slotBase[i]!, slots = table.slotBase[i + 1]! - base;
	let count = 0;
	for ( let slot = 0; slot < slots; slot++ ) {
		const descriptor = group.visibility?.[slot];
		const resident = !descriptor ||
			descriptor.cells.some( cell =>
				Math.abs( cell[0] - cellX ) <= descriptor.cellRadius &&
				Math.abs( cell[1] - cellZ ) <= descriptor.cellRadius
			);
		if ( resident ) table.resident[base + count++] = slot;
	}
	table.residentCount[i] = count;
	table.residentCell[i * 2] = cellX;
	table.residentCell[i * 2 + 1] = cellZ;
	return count;
}

/*
================
FADE_KEEPS_IN / FADE_KEEPS_OUT

fadeKeeps flags: objects in (state 2) stay in, objects out (state 0) stay
out. In those states advanceObjectFade changes only the frame stamp.
================
*/
export const FADE_KEEPS_IN = 1;
export const FADE_KEEPS_OUT = 2;

/*
================
fadeKeeps

Which steady states every object of instanced group i keeps for an eye at
eye: its eye distance less its radius stays inside (in) or outside (out)
its range for any placement of the group.
================
*/
export function fadeKeeps( table: WalkTable, i: number, eye: readonly number[], sceneryRange: number ): number {
	const s = i * 5, r = i * 4;
	const distance = Math.hypot(
		eye[0]! - table.sphere[s]!,
		eye[1]! - table.sphere[s + 1]!,
		eye[2]! - table.sphere[s + 2]!
	);
	const spread = table.sphere[s + 4]!;
	const low = Math.min( table.reach[r]!, sceneryRange + table.reach[r + 2]! ),
		high = Math.max( table.reach[r + 1]!, sceneryRange + table.reach[r + 3]! );
	return (distance + spread + REACH_MARGIN < low ? FADE_KEEPS_IN : 0) |
		(distance - spread - REACH_MARGIN > high ? FADE_KEEPS_OUT : 0);
}
