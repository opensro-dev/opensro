/*
===========================================================================

position-skill.ts - ground-targeted skill goals and requests

The goal a ground skill casts at (the picked ground, or a 500-unit camera
ray) and the 0x72CD request that carries it. The server owns range and
collision admission.

===========================================================================
*/

import type { Pose } from "@/engine/contracts/gameplay";
import type { GroundPickQuery } from "@/engine/contracts/navigation";

/*
================
positionSkillGoal

6FD536: use the picked ground, or a 500-unit horizontal camera-ray goal.
The server owns authored range and collision admission.
================
*/
export function positionSkillGoal(
	from: Pose,
	query: GroundPickQuery,
	picked: Omit<Pose, "angle"> | null
): Pose | null {
	if ( picked ) return { ...picked, angle: from.angle };
	const dx = query.ray.delta[0]!, dz = query.ray.delta[2]!, length = Math.hypot( dx, dz );
	if ( !Number.isFinite( length ) || !length ) return null;
	return { ...from, x: from.x + dx / length * 500, z: from.z + dz / length * 500 };
}

/*
================
positionSkillRequest
================
*/
export function positionSkillRequest( id: number, to: Pose ) {
	if (
		!Number.isInteger( id ) || id <= 0 || id > 0xffffffff || !Number.isInteger( to.regionId ) || to.regionId < 0 ||
		to.regionId > 65535
	) throw Error( "Invalid ground skill request" );
	const p = new Uint8Array( 15 ), v = new DataView( p.buffer );
	p[0] = 1;
	p[1] = 4;
	v.setUint32( 2, id, true );
	p[6] = 2;
	v.setUint16( 7, to.regionId, true );
	for ( const [i, n] of [ to.x, to.y, to.z ].entries() ) {
		if ( !Number.isFinite( n ) || Math.trunc( n ) < -32768 || Math.trunc( n ) > 32767 ) {
			throw Error( "Ground skill coordinate is out of range" );
		}
		v.setInt16( 9 + i * 2, Math.trunc( n ), true );
	}
	return { opcode: 0x72cd, payload: p };
}
