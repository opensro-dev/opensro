/*
===========================================================================

scroll.ts - linked text-box scroll position

Rendering supplies geometry; this owner retains fractional drag position.
Buttons, wheel and drag share one range and clamp boundary. Head-anchored
lists retain their first visible row as members arrive; messages follow the
tail after their owner resets the position on arrival.

===========================================================================
*/
import type { UiEvent, UiRect } from "@/engine/contracts/ui";

/*
================
createMessageScroll
================
*/
export function createMessageScroll( id: string, anchor: "tail" | "head" = "tail" ) {
	let position = 0, range = 0, travel = 0, bounds: UiRect = [ 0, 0, 0, 0 ];
	const clamp = ( value: number ) => Math.max( 0, Math.min( range, value ) );
	return {
		/*
		================
		offset
		================
		*/
		offset: () => Math.round( position ),
		/*
		================
		geometry
		================
		*/
		geometry( value: { range: number; travel: number; bounds: UiRect; } ) {
			if ( anchor === "head" ) position += value.range - range;
			range = value.range;
			travel = value.travel;
			bounds = value.bounds;
			position = clamp( position );
		},
		/*
		================
		reset
		================
		*/
		reset() {
			position = range = travel = 0;
			bounds = [ 0, 0, 0, 0 ];
		},
		/*
		================
		event
		================
		*/
		event( event: UiEvent ) {
			if ( event.kind === "activate" && (event.id === id + "-up" || event.id === id + "-down") ) {
				position = clamp( Math.round( position ) + (event.id === id + "-up" ? 1 : -1) );
				return true;
			}
			if ( event.kind === "drag" && event.id === id + "-thumb" ) {
				if ( travel > 0 ) position = clamp( position - event.dy * range / travel );
				return true;
			}
			if (
				event.kind === "scroll" && event.x >= bounds[0] && event.x < bounds[0] + bounds[2] &&
				event.y >= bounds[1] && event.y < bounds[1] + bounds[3]
			) {
				position = clamp( Math.round( position ) - Math.sign( event.delta ) * 3 );
				return true;
			}
			return false;
		}
	};
}
