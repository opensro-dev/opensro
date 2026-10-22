/*
===========================================================================

touch-inspect.ts - the held hover of a touch tap (GH #1243)

Port-only, not native: v1.150 has no touch input. A phone has no hover, so
an item or skill tooltip flashed for one frame and vanished. A touch tap on
a control with the inspect flag keeps its hover after the finger lifts,
until the next touch, a wheel or scroll, or the control losing the flag.
Mouse hover never sets it. The bridge decides when a tap qualifies and
owns the reported hover; this owner only holds and releases it.

===========================================================================
*/

/*
================
TouchInspectionHooks

hoverAt: the hover a touch landing on target reports in its place.
inspectable: the control still exists, carries the flag and is enabled.
report: report this hover, if it is not the one already reported.
================
*/
export type TouchInspectionHooks = {
	signal: AbortSignal;
	hoverAt: ( target: EventTarget | null ) => string | null;
	inspectable: ( id: string ) => boolean;
	report: ( id: string | null ) => void;
};

/*
================
createTouchInspection
================
*/
export function createTouchInspection( hooks: TouchInspectionHooks ) {
	let inspected: string | null = null;
	/*
	================
	release

	End the inspection and report the hover that replaces it.
	================
	*/
	function release( next: string | null = null ) {
		if ( inspected === null ) return;
		inspected = null;
		hooks.report( next );
	}
	window.addEventListener( "pointerdown", event => {
		if ( event.pointerType !== "touch" || inspected === null ) return;
		release( hooks.hoverAt( event.target ) );
	}, { capture: true, signal: hooks.signal } );
	for ( const name of [ "wheel", "scroll" ] as const ) {
		window.addEventListener( name, () => release(), { capture: true, passive: true, signal: hooks.signal } );
	}
	return {
		/*
		================
		inspect

		A qualifying tap on id: hold its hover.
		================
		*/
		inspect( id: string ) {
			inspected = id;
			hooks.report( id );
		},
		/*
		================
		holds

		True while a touch event's hover changes must leave the held hover
		alone (a lifting finger leaves its slot; the inspection outlives it).
		================
		*/
		holds( event: PointerEvent ) {
			return event.pointerType === "touch" && inspected !== null;
		},
		/*
		================
		sync

		After a publication: release the inspection if its control lost the
		flag. True while an inspection owns the hover, so the caller does not
		re-test the last touch point, which would re-report the released slot.
		================
		*/
		sync() {
			if ( inspected === null ) return false;
			if ( !hooks.inspectable( inspected ) ) release();
			return true;
		},
		/*
		================
		retire

		The control id left the tree.
		================
		*/
		retire( id: string ) {
			if ( inspected === id ) release();
		},
		get active() {
			return inspected !== null;
		}
	};
}
