/*
===========================================================================

viewer.ts - the 3D viewer: one model drawn alone, with no session

Port-only, not native (docs/VIEWER.md). runtime.ts startViewerRuntime
creates the owners (assets, renderer, character presentation) and calls
frame() once per animation frame; this module decides what each frame
shows and reports the outcome. The model is one entity fed through the
game's own character owner, so it resolves and loads exactly as in the
world; the picture is the inventory doll's transparent target, framed by
viewer-camera.ts and read back for the still.

The outcome goes to the embedding page: viewer-ready / viewer-still /
viewer-error messages to the parent or opener, and window.__viewerStill.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { Renderer } from "@/engine/contracts/runtime";
import type { UiQuad, UiScene } from "@/engine/contracts/ui";
import {
	parseViewerRequest,
	viewerError,
	viewerReason,
	type ViewerReason,
	type ViewerRequest
} from "@/engine/foundation/ui/viewer-request";
import { viewerCamera } from "@/engine/foundation/rendering/viewer-camera";

// The one entity's gid; nothing else exists on the page.
const VIEWER_GID = 1;
// A model that has not drawn within this many milliseconds fails "timeout".
const VIEWER_TIMEOUT_MS = 30000;
// Frames drawn after the model is resident before the readback: the first
// frame admits the doll's draws, the next one has them in the target.
const SETTLE_FRAMES = 2;
// Models face -z at the doll's yaw 0 (portrait.ts), so that is their front.
const MODEL_FRONT: readonly [number, number] = [ 0, -1 ];
const WHITE: readonly [number, number, number, number] = [ 1, 1, 1, 1 ];

declare global {
	interface Window {
		__viewerStill?: Promise<string>;
	}
}

/*
================
ViewerCharacters

The slice of the character presentation the viewer drives.
================
*/
export interface ViewerCharacters {
	receiveLifecycle( events: readonly import("@/engine/contracts/world").WorldEvent[] ): void;
	step( entities: readonly EntityState[], gameplay: null, seconds: number ): void;
	ready( gid: number ): boolean;
	knows( entity: EntityState ): boolean | undefined;
	error(): string | null | undefined;
}

/*
================
ViewerHost

What startViewerRuntime hands the viewer. The runtime owns their lifetime.
================
*/
export interface ViewerHost {
	readonly canvas: HTMLCanvasElement;
	readonly search: string;
	readonly renderer: Renderer;
	readonly characters: ViewerCharacters;
}

/*
================
post

A message to whoever embeds or opened the page; none when it stands alone.
================
*/
function post( message: Record<string, unknown> ) {
	const target = window.parent !== window ? window.parent : window.opener as Window | null;
	target?.postMessage( message, "*" );
}

/*
================
encodePng

The still as a PNG data URL, alpha kept.
================
*/
async function encodePng( still: { width: number; height: number; rgba: Uint8ClampedArray; } ): Promise<string> {
	const canvas = new OffscreenCanvas( still.width, still.height ), context = canvas.getContext( "2d" );
	if ( !context ) throw viewerError( "render" );
	context.putImageData( new ImageData( new Uint8ClampedArray( still.rgba ), still.width, still.height ), 0, 0 );
	const bytes = new Uint8Array( await (await canvas.convertToBlob( { type: "image/png" } )).arrayBuffer() );
	let binary = "";
	for ( let i = 0; i < bytes.length; i++ ) binary += String.fromCharCode( bytes[i]! );
	return "data:image/png;base64," + btoa( binary );
}

/*
================
viewerEntity

The monster as a world entity at the origin, facing its native front.
================
*/
function viewerEntity( refObjId: number ): EntityState {
	return { gid: VIEWER_GID, refObjId, kind: "monster", regionId: 0, x: 0, y: 0, z: 0, heading: 0, name: "" };
}

/*
================
dollScene

One full-size doll quad and nothing else.
================
*/
function dollScene( revision: number, size: number, doll: NonNullable<UiQuad["doll"]> ): UiScene {
	return {
		revision,
		width: size,
		height: size,
		quads: [ {
			doll,
			texture: "__doll",
			rect: [ 0, 0, size, size ],
			uv: [ 0, 0, 1, 1 ],
			color: WHITE,
			clip: [ 0, 0, size, size ]
		} ]
	};
}

/*
================
createViewer

Parses the request and returns the per-frame step. frame() answers
whether it wants another frame; once it says no the runtime disposes the
owners. Only the monster form draws so far: a look request is refused
"bad-request" until the character step lands (docs/VIEWER.md).
================
*/
export function createViewer( host: ViewerHost ) {
	// settle is the one way out of the still promise: a PNG or a reason.
	let settle: ( outcome: { png: string; } | { reason: ViewerReason; } ) => void = () => {};
	window.__viewerStill = new Promise<string>( ( resolve, reject ) => {
		settle = outcome => "png" in outcome ? resolve( outcome.png ) : reject( Error( outcome.reason ) );
	} );
	// An unawaited rejection is still reported through the message.
	window.__viewerStill.catch( () => {} );
	let request: ViewerRequest | null = null, failure: ViewerReason | null = null;
	try {
		request = parseViewerRequest( host.search );
	} catch ( error ) {
		failure = viewerReason( error );
	}
	if ( !failure && !("gpu" in navigator) ) failure = "no-webgpu";
	if ( !failure && request?.kind !== "monster" ) failure = "bad-request";
	const size = request?.size ?? 1, entity = viewerEntity( request?.kind === "monster" ? request.refObjId : 0 );
	const entities = [ entity ], viewport = { width: size, height: size };
	let started = -1, revision = 0, settled = 0, reading = false, finished = false;
	let camera: import("@/engine/contracts/scene").WorldCamera | null = null;
	/*
	================
	fail
	================
	*/
	const fail = ( reason: ViewerReason ) => {
		if ( finished ) return;
		finished = true;
		post( { type: "viewer-error", reason } );
		settle( { reason } );
	};
	/*
	================
	succeed
	================
	*/
	const succeed = ( png: string ) => {
		if ( finished ) return;
		finished = true;
		post( { type: "viewer-ready" } );
		if ( request?.still ) post( { type: "viewer-still", png } );
		settle( { png } );
	};
	if ( failure ) fail( failure );
	else {
		host.canvas.width = size;
		host.canvas.height = size;
		host.characters.receiveLifecycle( [ { kind: "spawn", entity } ] );
	}
	return {
		/*
		================
		frame

		Steps the presentation, frames the model once its bounds are known,
		draws, and reads the doll back after it settles.
		================
		*/
		frame( now: number ): boolean {
			if ( finished ) return false;
			if ( reading ) return true;
			if ( started < 0 ) started = now;
			if ( now - started > VIEWER_TIMEOUT_MS ) {
				fail( "timeout" );
				return false;
			}
			if ( host.renderer.phase() === "failed" ) {
				fail( "render" );
				return false;
			}
			if ( host.characters.knows( entity ) === false ) {
				fail( "unknown-monster" );
				return false;
			}
			if ( host.characters.error() ) {
				fail( "asset-load" );
				return false;
			}
			const seconds = (now - started) / 1000;
			host.characters.step( entities, null, seconds );
			if ( !camera && host.characters.ready( VIEWER_GID ) ) {
				const bounds = host.renderer.characterBounds?.( VIEWER_GID );
				// The doll turns the model by pi about y (portrait.ts previewYaw of
				// yaw 0), so model x and z change sign in the doll's space.
				if ( bounds ) {
					camera = viewerCamera(
						{ min: [ -bounds[3], bounds[1], -bounds[5] ], max: [ -bounds[0], bounds[4], -bounds[2] ] },
						MODEL_FRONT
					);
				}
			}
			if ( camera ) host.renderer.setUi( dollScene( ++revision, size, { gid: VIEWER_GID, yaw: 0, camera } ) );
			host.renderer.frame( viewport, seconds );
			if ( camera && ++settled >= SETTLE_FRAMES ) {
				reading = true;
				host.renderer.readDoll!().then( encodePng ).then( succeed, () => fail( "render" ) );
			}
			return true;
		}
	};
}
