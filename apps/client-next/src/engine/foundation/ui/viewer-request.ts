/*
===========================================================================

viewer-request.ts - what the embeddable 3D viewer is asked to draw

Port-only, not native (docs/VIEWER.md). Parses viewer.html's query into one
request, a monster by reference id or a character by the public API's look
object, and owns the fixed failure reasons the website and the catalogue
record. Pure: no browser, no assets.

===========================================================================
*/

/*
================
ViewerReason

The only failure reasons the viewer reports (viewerReasons lists them).
================
*/
export type ViewerReason = "no-webgpu" | "bad-request" | "unknown-monster" | "asset-load" | "render" | "timeout";

/*
================
viewerReasons
================
*/
export function viewerReasons(): readonly ViewerReason[] {
	return [ "no-webgpu", "bad-request", "unknown-monster", "asset-load", "render", "timeout" ];
}

// The still's side in pixels: VIEWER_STILL_SIZE unless &size= says otherwise.
export const VIEWER_STILL_SIZE = 384;
const MIN_STILL_SIZE = 64;
const MAX_STILL_SIZE = 1024;
// A look names at most this many worn and avatar items.
const MAX_LOOK_ITEMS = 32;
// A reference id is a positive u32.
const MAX_REF_ID = 0xffffffff;
// A worn slot is an equipment socket (0..12); an avatar slot one of four.
const MAX_WORN_SLOT = 12;
const MAX_AVATAR_SLOT = 3;
const MAX_PLUS = 255;
// The base64url text of a look is bounded before it is decoded.
const MAX_LOOK_TEXT = 4096;

/*
================
ViewerLook

The public API's P6 look: the body and what it wears.
================
*/
export interface ViewerLook {
	readonly bodyRefObjId: number;
	readonly worn: readonly { readonly slot: number; readonly refItemId: number; readonly plus: number; }[];
	readonly avatar: readonly { readonly slot: number; readonly refItemId: number; }[];
}

/*
================
ViewerRequest
================
*/
export type ViewerRequest =
	& ({ readonly kind: "monster"; readonly refObjId: number; } | { readonly kind: "look"; readonly look: ViewerLook; })
	& { readonly still: boolean; readonly size: number; };

/*
================
ViewerError

A failure carrying one of viewerReasons() as its message and reason.
================
*/
export type ViewerError = Error & { readonly reason: ViewerReason; };

/*
================
viewerError
================
*/
export function viewerError( reason: ViewerReason ): ViewerError {
	return Object.assign( new Error( reason ), { reason } );
}

/*
================
isViewerError
================
*/
function isViewerError( error: unknown ): error is ViewerError {
	return error instanceof Error && viewerReasons().includes( (error as Partial<ViewerError>).reason as ViewerReason );
}

/*
================
refId
================
*/
function refId( value: unknown ): number | null {
	if ( typeof value === "string" ) {
		if ( !/^[1-9][0-9]{0,9}$/.test( value ) ) return null;
		value = Number( value );
	}
	return Number.isInteger( value ) && (value as number) >= 1 && (value as number) <= MAX_REF_ID ?
		value as number :
		null;
}

/*
================
decodeBase64Url
================
*/
function decodeBase64Url( text: string ): string | null {
	if ( text.length > MAX_LOOK_TEXT || !/^[A-Za-z0-9_-]+={0,2}$/.test( text ) ) return null;
	try {
		const binary = atob( text.replace( /-/g, "+" ).replace( /_/g, "/" ) );
		return new TextDecoder( "utf-8", { fatal: true } ).decode( Uint8Array.from( binary, c => c.charCodeAt( 0 ) ) );
	} catch {
		return null;
	}
}

/*
================
parseLook
================
*/
function parseLook( text: string ): ViewerLook | null {
	const json = decodeBase64Url( text );
	if ( json === null ) return null;
	let value: unknown;
	try {
		value = JSON.parse( json );
	} catch {
		return null;
	}
	const raw = value as Partial<Record<"bodyRefObjId" | "worn" | "avatar", unknown>> | null;
	const body = refId( raw?.bodyRefObjId );
	const worn = raw?.worn ?? [], avatar = raw?.avatar ?? [];
	if (
		body === null || !Array.isArray( worn ) || !Array.isArray( avatar ) ||
		worn.length + avatar.length > MAX_LOOK_ITEMS
	) return null;
	const out: { slot: number; refItemId: number; plus: number; }[] = [];
	for ( const row of worn as Partial<Record<"slot" | "refItemId" | "plus", unknown>>[] ) {
		const item = refId( row?.refItemId ), slot = row?.slot, plus = row?.plus ?? 0;
		if (
			item === null || !Number.isInteger( slot ) || (slot as number) < 0 || (slot as number) > MAX_WORN_SLOT ||
			!Number.isInteger( plus ) || (plus as number) < 0 || (plus as number) > MAX_PLUS
		) return null;
		out.push( { slot: slot as number, refItemId: item, plus: plus as number } );
	}
	const dress: { slot: number; refItemId: number; }[] = [];
	for ( const row of avatar as Partial<Record<"slot" | "refItemId", unknown>>[] ) {
		const item = refId( row?.refItemId ), slot = row?.slot;
		if (
			item === null || !Number.isInteger( slot ) || (slot as number) < 0 || (slot as number) > MAX_AVATAR_SLOT
		) {
			return null;
		}
		dress.push( { slot: slot as number, refItemId: item } );
	}
	return { bodyRefObjId: body, worn: out, avatar: dress };
}

/*
================
parseViewerRequest

The request a query string names. Exactly one of monster and look; any
malformed value throws viewerError("bad-request").
================
*/
export function parseViewerRequest( search: string ): ViewerRequest {
	const query = new URLSearchParams( search );
	const monster = query.get( "monster" ), look = query.get( "look" );
	const still = query.get( "still" ), sizeText = query.get( "size" );
	if ( (monster === null) === (look === null) || (still !== null && still !== "1" && still !== "0") ) {
		throw viewerError( "bad-request" );
	}
	let size = VIEWER_STILL_SIZE;
	if ( sizeText !== null ) {
		size = /^[0-9]{2,4}$/.test( sizeText ) ? Number( sizeText ) : NaN;
		if ( !(size >= MIN_STILL_SIZE && size <= MAX_STILL_SIZE) ) throw viewerError( "bad-request" );
	}
	const common = { still: still === "1", size };
	if ( monster !== null ) {
		const id = refId( monster );
		if ( id === null ) throw viewerError( "bad-request" );
		return { kind: "monster", refObjId: id, ...common };
	}
	const parsed = parseLook( look! );
	if ( parsed === null ) throw viewerError( "bad-request" );
	return { kind: "look", look: parsed, ...common };
}

/*
================
viewerReason

The reason an unknown failure reports: its own when it is a viewerError,
otherwise "render".
================
*/
export function viewerReason( error: unknown ): ViewerReason {
	return isViewerError( error ) ? error.reason : "render";
}
