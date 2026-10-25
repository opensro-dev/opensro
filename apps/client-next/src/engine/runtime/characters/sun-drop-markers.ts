/*
===========================================================================

sun-drop-markers.ts - the admitted Seal of Sun drops' ground rings

Port-only, not native (#1640). The simulation stamps the player's own
degree-9+ Seal of Sun drops (groundItem.sunMarker); this owner watches
the frame's entities and publishes the marked poses to the world
renderer, which draws one gold ring under each. Publishes only when
the admitted set changes; clears with the presentation teardown.

===========================================================================
*/
import type { Renderer } from "@/engine/contracts/runtime";
import type { SunDropMarker } from "@/engine/contracts/sun-drop-marker";
import type { EntityState } from "@/engine/contracts/world";

/*
================
createSunDropMarkers
================
*/
export function createSunDropMarkers( renderer: Pick<Renderer, "setSunDropMarkers"> ) {
	let published = "";
	/*
	================
	step

	Publish this frame's admitted drops: the simulation's stamps, at
	their ground poses.
	================
	*/
	function step( entities: readonly EntityState[] ) {
		const markers: SunDropMarker[] = [];
		for ( const entity of entities ) {
			if ( entity.groundItem?.sunMarker ) {
				markers.push( {
					gid: entity.gid,
					pose: { regionId: entity.regionId, x: entity.x, y: entity.y, z: entity.z }
				} );
			}
		}
		const key = markers.map( marker => marker.gid ).join( "," );
		if ( key === published ) return;
		published = key;
		renderer.setSunDropMarkers( markers );
	}
	/*
	================
	clear
	================
	*/
	function clear() {
		if ( !published ) return;
		published = "";
		renderer.setSunDropMarkers( [] );
	}
	return { step, clear };
}
