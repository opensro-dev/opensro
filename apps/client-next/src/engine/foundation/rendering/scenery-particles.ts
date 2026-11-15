/*
===========================================================================

scenery-particles.ts - project BSR ModDataParticle records into SceneryEmitters

Converts a world object's ambient ModDataParticle (CModDataParticle_Load_ACD4D0) entries into
world-space SceneryEmitter records using the native row-vector Euler
orientation (AD05F0).

A non-zero field00 is the object root: vector3c offsets it. A zero field00
names a bone of the object's skeleton; the publisher writes that bone's
bind matrix on the entry (scripts/build/world/objects/sceneryBones.mjs),
and the bone matrix owns the translation, so vector3c does not apply
(AEC870, as bsrParticleTransform for characters).

===========================================================================
*/

import type { SceneryEmitter } from "@/engine/contracts/scenery";
import { radians } from "@/engine/foundation/math/angles";
import { multiply } from "@/engine/foundation/math/pose-math";
import { identity } from "./world-math";

const MAX_SCENERY_PARTICLE_MODIFIERS = 128;
// jinsi_floor01.bsr (Qin-Shi Tomb B1, 0x8007) carries 174 ambient particle
// entries in a single ModDataParticle record.
const MAX_SCENERY_PARTICLE_ENTRIES = 512;
const BONE_MATRIX_LENGTH = 16;

/*
================
sceneryOrientation

AD05F0: native row-vector Euler matrix, stored column-major for our math.
================
*/
export function sceneryOrientation( v: readonly number[] ): Float32Array {
	if ( v.length !== 3 || !v.every( Number.isFinite ) ) {
		throw Error( "Invalid emitter orientation" );
	}
	const [x, y, z] = v as readonly [number, number, number];
	const cx = Math.fround( Math.cos( x ) );
	const sx = Math.fround( Math.sin( x ) );
	const cy = Math.fround( Math.cos( y ) );
	const sy = Math.fround( Math.sin( y ) );
	const cz = Math.fround( Math.cos( z ) );
	const sz = Math.fround( Math.sin( z ) );
	const m = identity();
	const a = Math.fround( sy * cx );
	const b = Math.fround( sy * sx );
	m[0] = cz * cy;
	m[1] = -cy * sz;
	m[2] = -sy;
	m[4] = sz * cx - b * cz;
	m[5] = b * sz + cz * cx;
	m[6] = -sx * cy;
	m[8] = a * cz + sz * sx;
	m[9] = sx * cz - sz * a;
	m[10] = cy * cx;
	return m;
}

/*
================
sceneryParticles
================
*/
export function sceneryParticles(
	value: unknown,
	placement: string,
	branch: number,
	region: number,
	matrix: Float32Array,
	warn: ( message: string ) => void
): SceneryEmitter[] {
	if ( value === undefined ) return [];
	if ( !Array.isArray( value ) || value.length > MAX_SCENERY_PARTICLE_MODIFIERS ) {
		throw Error( "Invalid scenery particle modifiers" );
	}
	const out: SceneryEmitter[] = [];
	const modifiers: readonly unknown[] = value;
	/*
	================
	record
	================
	*/
	const record = ( row: unknown ): Record<string, unknown> => {
		if ( !row || typeof row !== "object" || Array.isArray( row ) ) {
			throw Error( "Invalid scenery modifier record" );
		}
		return row as Record<string, unknown>;
	};
	/*
	================
	vector
	================
	*/
	const vector = ( row: unknown ): readonly number[] => {
		if ( !Array.isArray( row ) || row.length !== 3 ) {
			throw Error( "Invalid scenery emitter vector" );
		}
		const values: readonly unknown[] = row;
		if ( !values.every( ( v ) => typeof v === "number" && Number.isFinite( v ) ) ) {
			throw Error( "Invalid scenery emitter vector" );
		}
		return values as readonly number[];
	};
	/*
	================
	boneMatrix

	The published bind matrix of a bone entry, or null when the skeleton
	lacks the bone. A missing socket keeps its parent matrix
	(CRTSocket_UpdateOrdinaryMatrices AB68C0), so the caller falls back
	to the object root.
	================
	*/
	const boneMatrix = ( row: unknown ): Float32Array | null => {
		if ( row === undefined ) return null;
		if ( !Array.isArray( row ) || row.length !== BONE_MATRIX_LENGTH ) {
			throw Error( "Invalid scenery emitter bone matrix" );
		}
		const values: readonly unknown[] = row;
		if ( !values.every( ( v ) => typeof v === "number" && Number.isFinite( v ) ) ) {
			throw Error( "Invalid scenery emitter bone matrix" );
		}
		return new Float32Array( values as readonly number[] );
	};
	for ( const [mi, raw] of modifiers.entries() ) {
		const m = record( raw );
		if ( m.kind !== 2 || m.stateId !== -1 || m.animationSetName !== "ambient" ) {
			warn( "Unsupported scenery particle state" );
			continue;
		}
		if ( !Array.isArray( m.entries ) || m.entries.length > MAX_SCENERY_PARTICLE_ENTRIES ) {
			throw Error( "Invalid scenery emitter list" );
		}
		const entries: readonly unknown[] = m.entries;
		for ( const [ei, rawEntry] of entries.entries() ) {
			const e = record( rawEntry );
			const position = vector( e.vector3c );
			const flags = vector( e.flags50 );
			if ( flags.some( ( v ) => !Number.isInteger( v ) || v < 0 || v > 255 ) ) {
				throw Error( "Invalid scenery emitter parameters" );
			}
			let bone: Float32Array | null = null;
			if ( !e.field00 ) {
				bone = boneMatrix( e.boneMatrix );
				if ( !bone ) warn( "Scenery emitter bone absent; kept at the object root" );
			}
			if ( e.field4c !== 0 ) {
				warn( "Unsupported scenery emitter timing" );
				continue;
			}
			if ( typeof e.effectPath !== "string" ) {
				throw Error( "Invalid scenery emitter resource" );
			}
			const path = e.effectPath.replaceAll( "\\", "/" ).toLowerCase();
			if (
				!path.endsWith( ".efp" ) ||
				path.startsWith( "/" ) ||
				path.includes( ".." ) ||
				path.includes( ":" )
			) {
				throw Error( "Invalid scenery emitter resource" );
			}
			const [x, y, z] = bone ? [ 0, 0, 0 ] : position as readonly [number, number, number];
			const holder = identity();
			if ( bone ) multiply( matrix, bone, holder );
			else holder.set( matrix );
			const orientation = e.flag53 ? sceneryOrientation( vector( e.vector54 ) ) : identity();
			const basis = identity();
			multiply( holder, orientation, basis );
			out.push( {
				id: `${placement}:${branch}:${mi}:${ei}`,
				placement,
				model: "/assets/effects/programs.json#" + encodeURIComponent( path ),
				pose: {
					regionId: region,
					x: holder[12]! + holder[0]! * x + holder[4]! * y + holder[8]! * z,
					y: holder[13]! + holder[1]! * x + holder[5]! * y + holder[9]! * z,
					z: holder[14]! + holder[2]! * x + holder[6]! * y + holder[10]! * z,
					yaw: radians( 0 )
				},
				basis: [
					basis[0]!,
					basis[1]!,
					basis[2]!,
					basis[4]!,
					basis[5]!,
					basis[6]!,
					basis[8]!,
					basis[9]!,
					basis[10]!
				],
				nightOnly: !!(flags[1]! & 1),
				renderPriority: flags[0]!
			} );
		}
	}
	return out;
}

/*
================
holderSphere

The bounding sphere, in its own frame, of a compound whose scenery emitters
are attached to it: around the box of every branch's resident meshes (keyed
by lower-case path), [x, y, z, radius]; null when none is resident.
================
*/
export function holderSphere(
	branches: readonly {
		readonly renderMeshSection?: { readonly paths: readonly string[]; };
		readonly meshPaths: readonly string[];
	}[],
	meshes: ReadonlyMap<
		string,
		{ readonly bounds: { readonly min: readonly number[]; readonly max: readonly number[]; }; }
	>
): readonly number[] | null {
	const low = [ Infinity, Infinity, Infinity ], high = [ -Infinity, -Infinity, -Infinity ];
	let found = false;
	for ( const branch of branches ) {
		for ( const path of branch.renderMeshSection?.paths ?? branch.meshPaths ) {
			const box = meshes.get( path.toLowerCase() )?.bounds;
			if ( !box ) continue;
			found = true;
			for ( let axis = 0; axis < 3; axis++ ) {
				low[axis] = Math.min( low[axis]!, box.min[axis]! );
				high[axis] = Math.max( high[axis]!, box.max[axis]! );
			}
		}
	}
	if ( !found ) return null;
	return [
		(low[0]! + high[0]!) / 2,
		(low[1]! + high[1]!) / 2,
		(low[2]! + high[2]!) / 2,
		Math.hypot( high[0]! - low[0]!, high[1]! - low[1]!, high[2]! - low[2]! ) / 2
	];
}

/*
================
placeSceneryEmitters

Appends a placement's emitters to the scene's list, with the dungeon room
they belong to and their compound's sphere placed by its matrix.
================
*/
export function placeSceneryEmitters(
	out: SceneryEmitter[],
	emitters: readonly SceneryEmitter[],
	dungeonBlock: number | undefined,
	holder: readonly number[] | null | undefined,
	matrix: ArrayLike<number>
) {
	const placed = holder && emitters.length ? placedSphere( holder, matrix ) : undefined;
	for ( const emitter of emitters ) {
		out.push( {
			...emitter,
			...(dungeonBlock === undefined ? {} : { dungeonBlock }),
			...(placed ? { holder: placed } : {})
		} );
	}
}

/*
================
placedSphere

A compound's sphere placed by its placement matrix (column-major): the
centre transformed, the radius grown by the matrix's largest axis scale.
================
*/
export function placedSphere(
	local: readonly number[],
	matrix: ArrayLike<number>
): readonly [number, number, number, number] {
	const [x, y, z, radius] = local as [number, number, number, number];
	const scale = Math.max(
		Math.hypot( matrix[0]!, matrix[1]!, matrix[2]! ),
		Math.hypot( matrix[4]!, matrix[5]!, matrix[6]! ),
		Math.hypot( matrix[8]!, matrix[9]!, matrix[10]! )
	);
	return [
		matrix[0]! * x + matrix[4]! * y + matrix[8]! * z + matrix[12]!,
		matrix[1]! * x + matrix[5]! * y + matrix[9]! * z + matrix[13]!,
		matrix[2]! * x + matrix[6]! * y + matrix[10]! * z + matrix[14]!,
		radius * scale
	];
}
