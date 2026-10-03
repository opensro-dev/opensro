/*
===========================================================================

terrain-visibility.ts - frustum masks of terrain cell volumes

Material and LOD slices of one terrain cell frequently share the exact same
bound, so volumes are admitted once and tested once a frame into a mask
the selection walk reads by slot. A box around everything admitted lets
begin() reject a whole region behind the camera in one test.

===========================================================================
*/
import { visibleFrustumSphere, visibleFrustumAabb } from "./world-math";

type Volume = {
	readonly bounds?: readonly [number, number, number, number, number, number];
	readonly center: readonly [number, number, number];
	readonly radius: number;
};

// A box grown by this much rejects only when every volume inside it would:
// the frustum tests' float headroom is a few hundredths of a unit here.
const VOLUME_BOX_MARGIN = 1;

/*
================
growVolumeBox

Extends box (min xyz, max xyz) to hold volume.
================
*/
function growVolumeBox( box: number[], volume: Volume ) {
	const b = volume.bounds, c = volume.center, r = volume.radius;
	for ( let axis = 0; axis < 3; axis++ ) {
		const low = b ? b[axis]! : c[axis]! - r, high = b ? b[axis + 3]! : c[axis]! + r;
		box[axis] = Math.min( box[axis]!, low - VOLUME_BOX_MARGIN );
		box[axis + 3] = Math.max( box[axis + 3]!, high + VOLUME_BOX_MARGIN );
	}
}

/*
================
createTerrainVisibility
================
*/
export function createTerrainVisibility() {
	const shared = new Map<string, number>(), entries = new WeakMap<Volume, number>(), volumes: Volume[] = [];
	const box = [ Infinity, Infinity, Infinity, -Infinity, -Infinity, -Infinity ];
	let mask = new Uint8Array( 0 ), evaluations = 0;
	return {
		/*
		================
		admit
		================
		*/
		admit( volume: Volume ) {
			if ( entries.has( volume ) ) return;
			const key = volume.bounds ?
				"box:" + volume.bounds.join( "," ) :
				"sphere:" + volume.center.join( "," ) + "," + volume.radius;
			let entry = shared.get( key );
			if ( entry === undefined ) {
				entry = volumes.length;
				volumes.push( volume );
				shared.set( key, entry );
				growVolumeBox( box, volume );
				if ( volumes.length > mask.length ) {
					const next = new Uint8Array( 2 ** Math.ceil( Math.log2( volumes.length ) ) );
					next.set( mask );
					mask = next;
				}
			}
			entries.set( volume, entry );
		},
		/*
		================
		indices

		Compiles immutable material slices once. Render-rate consumers read
		numeric slots instead of repeating WeakMap lookups for every
		material/LOD range.
		================
		*/
		indices( values: readonly Volume[] ) {
			return Uint32Array.from( values, volume => {
				const index = entries.get( volume );
				if ( index === undefined ) throw Error( "Terrain volume was not admitted" );
				return index;
			} );
		},
		/*
		================
		begin
		================
		*/
		begin( frustum: Float64Array ) {
			evaluations = volumes.length;
			if ( !volumes.length || !visibleFrustumAabb( frustum, box ) ) {
				mask.fill( 0, 0, volumes.length );
				return mask;
			}
			for ( let i = 0; i < volumes.length; i++ ) {
				const volume = volumes[i]!, center = volume.center;
				mask[i] = Number(
					volume.bounds ?
						visibleFrustumAabb( frustum, volume.bounds ) :
						visibleFrustumSphere( frustum, center[0], center[1], center[2], volume.radius )
				);
			}
			return mask;
		},
		/*
		================
		visible
		================
		*/
		visible( volume: Volume ) {
			const entry = entries.get( volume );
			if ( entry === undefined ) throw Error( "Terrain volume was not admitted" );
			return mask[entry] === 1;
		},
		stats: () => ({ volumes: shared.size, evaluations })
	};
}
