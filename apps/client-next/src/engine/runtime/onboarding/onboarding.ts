/*
===========================================================================

onboarding.ts - the first-login tour of the port's additions

Plain DOM over the canvas, like the FPS chip and the bug reporter: the
tour explains tools around the original interface and is not part of it.
It dims the screen, leaves a lit window around one element, points at it
and says what it is for (onboarding-steps.ts chooses the step).

It runs only when the Agent says so (GET /title/onboarding, the deployer's
SRO_ONBOARDING), once the world is up and has been on screen for
START_DELAY_MS, so the native entry popups come first. Steps seen are kept
in this browser; "?tour" in the page address forgets them. While a step is
up, keys stop at the tour: Enter goes on, Escape ends the tour.

The frame clock drives the settings request, the search for the next step
and the window's position; this owner keeps no timer.

===========================================================================
*/

import { RELEASE_PROTOCOL, RELEASE_PROTOCOL_HEADER } from "@/engine/foundation/release/protocol";
import { nextStep, parseSeen, tourSteps, type TourStep } from "@/engine/foundation/ui/onboarding-steps";

const ROUTE = "/title/onboarding";
const STORAGE_KEY = "sro:onboarding:1";
// The world must be on screen this long before the first step.
const START_DELAY_MS = 4000;
// How often a hidden tour looks for an element that came on screen.
const SEARCH_INTERVAL_MS = 1000;
const SETTINGS_RETRY_MS = 30 * 1000;
// Lit margin around the element, and the gap between it and the text.
const SPOT_PADDING = 6;
const BUBBLE_GAP = 14;
const BUBBLE_MARGIN = 8;

/*
================
storedSeen
================
*/
function storedSeen() {
	try {
		if ( new URLSearchParams( location.search ).has( "tour" ) ) localStorage.removeItem( STORAGE_KEY );
		return parseSeen( localStorage.getItem( STORAGE_KEY ) );
	} catch {
		return new Set<string>();
	}
}

/*
================
onScreen

The element exists, nothing hides it, and it has a box inside the window.
================
*/
function onScreen( selector: string ) {
	const element = document.querySelector( selector );
	if ( !element || element.closest( "[hidden]" ) ) return false;
	const box = element.getBoundingClientRect();
	return box.width > 0 && box.height > 0 && box.right > 0 && box.bottom > 0 && box.left < innerWidth &&
		box.top < innerHeight;
}

/*
================
createOnboarding
================
*/
export function createOnboarding( apiBase: string ) {
	const lifetime = new AbortController(), signal = lifetime.signal, seen = storedSeen(), steps = tourSteps();
	let enabled: boolean | null = null, loading = false, retryAtMs = 0;
	let readySinceMs: number | null = null, searchAtMs = 0, current: TourStep | null = null;
	const root = document.createElement( "div" ),
		spot = document.createElement( "div" ),
		arrow = document.createElement( "div" ),
		bubble = document.createElement( "section" ),
		title = document.createElement( "h2" ),
		text = document.createElement( "p" ),
		actions = document.createElement( "div" ),
		skip = document.createElement( "button" ),
		next = document.createElement( "button" );
	root.className = "sro-tour";
	root.hidden = true;
	spot.className = "sro-tour__spot";
	arrow.className = "sro-tour__arrow";
	bubble.className = "sro-tour__bubble";
	bubble.setAttribute( "role", "dialog" );
	bubble.setAttribute( "aria-live", "polite" );
	title.className = "sro-tour__title";
	text.className = "sro-tour__text";
	actions.className = "sro-tour__actions";
	skip.type = next.type = "button";
	skip.className = "sro-tour__skip";
	skip.textContent = "Skip tour";
	next.className = "sro-tour__next";
	next.textContent = "Next";
	actions.append( skip, next );
	bubble.append( title, text, actions );
	root.append( spot, arrow, bubble );
	document.body.append( root );
	next.addEventListener( "click", () => advance(), { signal } );
	skip.addEventListener( "click", () => endTour(), { signal } );
	// The game reads keys on window; the tour takes them first while it is up.
	for ( const kind of [ "keydown", "keyup", "keypress" ] as const ) {
		window.addEventListener( kind, event => {
			if ( root.hidden ) return;
			event.stopPropagation();
			event.preventDefault();
			if ( kind !== "keydown" ) return;
			const key = (event as KeyboardEvent).key;
			if ( key === "Escape" ) endTour();
			else if ( key === "Enter" ) advance();
		}, { signal, capture: true } );
	}

	/*
	================
	loadSettings

	A promise chain, not an async function: the frame starts it.
	================
	*/
	function loadSettings( nowMs: number ) {
		loading = true;
		retryAtMs = nowMs + SETTINGS_RETRY_MS;
		fetch( apiBase + ROUTE, {
			headers: { [RELEASE_PROTOCOL_HEADER]: String( RELEASE_PROTOCOL ) },
			credentials: "omit",
			cache: "no-store",
			redirect: "error",
			signal
		} ).then( response =>
			response.json().then( ( body: { onboarding?: { enabled?: unknown; }; } ) => {
				if ( response.ok && typeof body.onboarding?.enabled === "boolean" ) enabled = body.onboarding.enabled;
			} )
		).catch( () => {
			// Asked again after SETTINGS_RETRY_MS; no tour until the Agent answers.
		} ).finally( () => {
			loading = false;
		} );
	}

	/*
	================
	remember
	================
	*/
	function remember() {
		try {
			localStorage.setItem( STORAGE_KEY, JSON.stringify( [ ...seen ] ) );
		} catch {
			// A browser without storage shows the tour again next time.
		}
	}

	/*
	================
	show
	================
	*/
	function show( step: TourStep | null ) {
		current = step;
		root.hidden = !step;
		if ( !step ) return;
		title.textContent = step.title;
		text.textContent = step.text;
		place();
		next.focus( { preventScroll: true } );
	}

	/*
	================
	advance

	The current step is seen; the next one on screen follows at once.
	================
	*/
	function advance() {
		if ( !current ) return;
		seen.add( current.id );
		remember();
		show( nextStep( steps, seen, onScreen ) );
	}

	/*
	================
	endTour

	Skipping ends the whole tour, the steps not reached yet included.
	================
	*/
	function endTour() {
		for ( const step of steps ) seen.add( step.id );
		remember();
		show( null );
	}

	/*
	================
	place

	The lit window over the element, the text below it (above when there is
	no room), and the arrow between them.
	================
	*/
	function place() {
		const element = current && document.querySelector( current.target );
		if ( !element || !onScreen( current!.target ) ) {
			// The element went away (a party left): keep the step for later.
			show( null );
			return;
		}
		const elementBox = element.getBoundingClientRect(), reach = elementBox.height * (current!.reachAbove ?? 0);
		const box = new DOMRect( elementBox.x, elementBox.y - reach, elementBox.width, elementBox.height + reach );
		spot.style.left = box.left - SPOT_PADDING + "px";
		spot.style.top = box.top - SPOT_PADDING + "px";
		spot.style.width = box.width + SPOT_PADDING * 2 + "px";
		spot.style.height = box.height + SPOT_PADDING * 2 + "px";
		const width = bubble.offsetWidth, height = bubble.offsetHeight;
		const below = box.bottom + SPOT_PADDING + BUBBLE_GAP + height + BUBBLE_MARGIN <= innerHeight;
		const bubbleTop = below ?
			box.bottom + SPOT_PADDING + BUBBLE_GAP :
			Math.max( BUBBLE_MARGIN, box.top - SPOT_PADDING - BUBBLE_GAP - height );
		const centre = box.left + box.width / 2;
		const bubbleLeft = Math.max(
			BUBBLE_MARGIN,
			Math.min( innerWidth - width - BUBBLE_MARGIN, centre - width / 2 )
		);
		bubble.style.left = bubbleLeft + "px";
		bubble.style.top = bubbleTop + "px";
		arrow.dataset.side = below ? "below" : "above";
		arrow.style.left = centre + "px";
		arrow.style.top = (below ? box.bottom + SPOT_PADDING : box.top - SPOT_PADDING - BUBBLE_GAP) + "px";
	}

	return {
		/*
		================
		step

		Called once per frame; `ready` is true while the world is on screen and
		nothing is loading.
		================
		*/
		step( nowMs: number, ready: boolean ) {
			if ( signal.aborted ) return;
			if ( enabled === null ) {
				if ( !loading && nowMs >= retryAtMs ) loadSettings( nowMs );
				return;
			}
			if ( !enabled ) return;
			if ( !ready ) {
				readySinceMs = null;
				show( null );
				return;
			}
			readySinceMs ??= nowMs;
			if ( current ) {
				place();
				return;
			}
			if ( nowMs - readySinceMs < START_DELAY_MS || nowMs < searchAtMs ) return;
			searchAtMs = nowMs + SEARCH_INTERVAL_MS;
			if ( steps.every( step => seen.has( step.id ) ) ) return;
			show( nextStep( steps, seen, onScreen ) );
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			lifetime.abort();
			root.remove();
		}
	};
}
