/*
===========================================================================

drag-shield.ts - the drag-lifetime gesture shield (GH #1242)

While a touch drag runs, its surface owns every other touch: a second
finger landing on a plain control (touch-action auto) is gesture-eligible,
and the browser can take the pair over as a pinch-zoom, cancelling the
drag. This owner raises a full-screen touch-action:none shield for
exactly the drag's lifetime. The drag pointer itself is captured to its
control and never hit-tests the shield. A mouse drag cannot pinch, so it
is never shielded and its chord presses keep reaching the controls. iOS
Safari opens pinch with gesturestart, ahead of touch-action, so that is
refused too - only while a touch drag runs.

===========================================================================
*/

/*
================
createDragShield
================
*/
export function createDragShield( signal: AbortSignal ) {
	const element = document.createElement( "div" );
	element.setAttribute( "aria-hidden", "true" );
	element.style.cssText = "position:fixed;inset:0;display:none;touch-action:none";
	element.addEventListener( "contextmenu", event => event.preventDefault(), { signal } );
	document.body.append( element );
	let active = false;
	window.addEventListener( "gesturestart", event => {
		if ( active ) event.preventDefault();
	}, { signal } );
	return {
		/*
		================
		set

		Raise or lower the shield with the drag state. Writes the style
		only on a change.
		================
		*/
		set( value: boolean ) {
			active = value;
			const display = value ? "block" : "none";
			if ( element.style.display !== display ) element.style.display = display;
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			element.remove();
		}
	};
}
