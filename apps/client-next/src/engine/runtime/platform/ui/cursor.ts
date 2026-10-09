/*
===========================================================================

cursor.ts - the retail mouse cursor, presented by the browser

Retail 0xA15AF0 uses SetCursor. Give the browser the extracted cursor and
hotspot; it owns pointer presentation independently of game frames outside
editor interaction. Admit the image on editor hover, before a click can
focus it and before Windows can hide the native pointer. Keep that owner
through focused editing/selection; never hand off on keys. The cursor files
are browser-loaded PNGs decoded from the native DIBs
(scripts/extract_client_cursors.py): every browser draws a PNG cursor, and
Safari drew no .cur at all (BUG-068).

===========================================================================
*/
import { cursorAssetUrl } from "@/engine/foundation/assets/native-assets";
import type { WorldCursor } from "@/engine/foundation/ui/world-cursor";

const DEFAULT_CURSOR: WorldCursor = 0x95;
const EDITABLE_INPUT_TYPES = [
	"text",
	"search",
	"url",
	"tel",
	"email",
	"password",
	"number",
	"date",
	"datetime-local",
	"month",
	"time",
	"week"
];

/*
================
hotspotOf

The hotspot extracted with each retail cursor resource.
================
*/
function hotspotOf( value: WorldCursor ): string {
	if ( value === 0x96 ) return "11 10";
	if ( value === 0x98 ) return "2 26";
	if ( value === 0x99 ) return "9 6";
	if ( value === 0x9a ) return "9 4";
	if ( value === 0xa6 ) return "0 0";
	if ( value === 0xa1 ) return "0 0";
	if ( value === 0xa3 ) return "15 15";
	return "2 1";
}

/*
================
cursorRule
================
*/
function cursorRule( value: WorldCursor ): string {
	return `html,html *{cursor:url("${cursorAssetUrl( value )}") ${hotspotOf( value )},auto!important}` +
		`html[data-sro-cursor-mode="editing"],html[data-sro-cursor-mode="editing"] *{cursor:none!important}`;
}

/*
================
editable
================
*/
function editable( target: EventTarget | null ): boolean {
	if ( target instanceof HTMLInputElement ) return !target.disabled && EDITABLE_INPUT_TYPES.includes( target.type );
	if ( target instanceof HTMLTextAreaElement ) return !target.disabled;
	return target instanceof HTMLElement && target.isContentEditable;
}

/*
================
createCursor
================
*/
export function createCursor() {
	const lifetime = new AbortController(),
		style = document.createElement( "style" ),
		typing = document.createElement( "img" );
	style.textContent = cursorRule( DEFAULT_CURSOR );
	typing.alt = "";
	typing.setAttribute( "aria-hidden", "true" );
	typing.dataset.typingCursor = "";
	typing.hidden = true;
	typing.style.cssText =
		"position:fixed;left:0;top:0;pointer-events:none;z-index:2147483647;image-rendering:pixelated;will-change:transform";
	let point: readonly [number, number] | null = null;
	let worldCursor: WorldCursor = DEFAULT_CURSOR, shownCursor: WorldCursor = DEFAULT_CURSOR, mouseButtons = 0;

	/*
	================
	world
	================
	*/
	function world() {
		const value = worldCursor === 0x99 && (mouseButtons & 1) ? 0x9a : worldCursor;
		if ( value === shownCursor ) return;
		shownCursor = value;
		document.documentElement.dataset.sroWorldCursor = value.toString( 16 );
		style.textContent = cursorRule( value );
	}

	/*
	================
	present

	hit is the element under the pointer when the caller already knows it
	(a pointer event's own hit test); otherwise the current point is
	hit-tested here. elementFromPoint forces a synchronous layout, so it must
	not run on every pointer move.
	================
	*/
	function present( target: EventTarget | null = document.activeElement, hit?: Element | null ) {
		// Focus is not the start of an editor gesture: pointer entry precedes it.
		// Hit-test the current point rather than retaining a potentially removed UI
		// element or trusting event.target while an element owns pointer capture.
		const hovered = hit !== undefined ? hit : point ? document.elementFromPoint( point[0], point[1] ) : null;
		const editing = !!point && !document.hidden && typing.complete && typing.naturalWidth > 0 &&
			(editable( target ) || editable( hovered ));
		if ( editing ) {
			const transform = `translate(${point![0] - 2}px, ${point![1] - 1}px)`;
			if ( typing.style.transform !== transform ) typing.style.transform = transform;
		}
		if ( typing.hidden === editing ) typing.hidden = !editing;
		if ( editing ) {
			if ( document.documentElement.dataset.sroCursorMode !== "editing" ) {
				document.documentElement.dataset.sroCursorMode = "editing";
			}
		} else if ( document.documentElement.dataset.sroCursorMode ) {
			delete document.documentElement.dataset.sroCursorMode;
		}
	}

	/*
	================
	leave
	================
	*/
	function leave() {
		point = null;
		mouseButtons = 0;
		world();
		present();
	}

	/*
	================
	pointer
	================
	*/
	function pointer( event: PointerEvent ) {
		if ( event.pointerType !== "mouse" ) {
			if ( event.type === "pointerdown" ) leave();
			return;
		}
		point = [ event.clientX, event.clientY ];
		mouseButtons = event.buttons;
		world();
		// An uncaptured event's target is the browser's own hit at this point.
		// While an element holds capture (the camera drag on the canvas, a
		// dragged control) no editor under the pointer can take the gesture, so
		// none is hovered.
		const target = event.target instanceof Element ? event.target : null;
		present( document.activeElement, target && !target.hasPointerCapture( event.pointerId ) ? target : null );
	}

	typing.addEventListener( "load", () => present(), { signal: lifetime.signal } );
	typing.addEventListener( "error", () => present(), { signal: lifetime.signal } );
	window.addEventListener( "pointerover", pointer, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "pointermove", pointer, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "pointerdown", pointer, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "pointerup", pointer, { signal: lifetime.signal, capture: true } );
	window.addEventListener( "focusin", event => present( event.target ), { signal: lifetime.signal } );
	window.addEventListener( "focusout", event => present( event.relatedTarget ), { signal: lifetime.signal } );
	window.addEventListener( "pointercancel", leave, { signal: lifetime.signal } );
	window.addEventListener( "blur", leave, { signal: lifetime.signal } );
	document.documentElement.addEventListener( "pointerleave", leave, { signal: lifetime.signal } );
	document.addEventListener( "visibilitychange", () => {
		if ( document.hidden ) leave();
	}, { signal: lifetime.signal } );
	document.head.append( style );
	document.body.append( typing );
	typing.src = cursorAssetUrl( DEFAULT_CURSOR );
	return {
		world( value: WorldCursor ) {
			worldCursor = value;
			world();
		},
		dispose() {
			lifetime.abort();
			typing.remove();
			style.remove();
			delete document.documentElement.dataset.sroCursorMode;
			delete document.documentElement.dataset.sroWorldCursor;
		}
	};
}
