/*
===========================================================================

viewer.ts - the 3D viewer: one model drawn alone, with no session

Port-only, not native (docs/VIEWER.md). runtime.ts startViewerRuntime
creates the owners (assets, renderer, character presentation) and calls
frame() once per animation frame; this module decides what each frame
shows and reports the outcome. The model is one entity fed through the
game's own character owner, so it resolves and loads exactly as in the
world; the picture is the inventory doll's transparent target, framed by
viewer-camera.ts and read back: once for a still, or after every change
into the page's picture canvas for the interactive view (drag turns the
model, the wheel zooms). The interactive view draws only while something
changes: frame() answers "idle" and input wakes the runtime's loop.

The outcome goes to the embedding page: viewer-ready / viewer-still /
viewer-error messages to the parent or opener, and window.__viewerStill.

===========================================================================
*/
import type { EntityEquipment, EntityState } from "@/engine/contracts/world";
import type { Renderer } from "@/engine/contracts/runtime";
import type { UiQuad, UiScene } from "@/engine/contracts/ui";
import type { WorldCamera } from "@/engine/contracts/scene";
import {
	parseViewerRequest,
	viewerError,
	viewerReason,
	type ViewerLook,
	type ViewerReason,
	type ViewerRequest,
	type ViewerStage
} from "@/engine/foundation/ui/viewer-request";
import {
	refitCamera,
	viewerCamera,
	type ViewerBounds,
	type ViewerFill
} from "@/engine/foundation/rendering/viewer-camera";
import { opaqueBox } from "@/engine/foundation/rendering/texel-readback";

// The one entity's gid; nothing else exists on the page.
const VIEWER_GID = 1;
// A model that has not drawn within this many milliseconds of drawing time
// fails "timeout". One frame counts at most MAX_FRAME_STEP_MS, so a page that
// sat hidden (no animation frames) does not time out the moment it shows.
const VIEWER_TIMEOUT_MS = 30000;
const MAX_FRAME_STEP_MS = 100;
// Frames drawn after the model is resident before the readback: the first
// frame admits the doll's draws, the next one has them in the target.
const SETTLE_FRAMES = 2;
// The interactive view: radians of turn per dragged pixel, the zoom factor
// per wheel pixel, and the zoom range around the framed distance.
const YAW_PER_PIXEL = 0.01;
const ZOOM_PER_WHEEL_PIXEL = 0.001;
const MIN_ZOOM = 0.4, MAX_ZOOM = 2.5;
// How the drawn model fills the picture after the refit: a portrait view
// by its height (a long weapon may leave the sides), a still whole.
const VIEW_FILL: ViewerFill = { kind: "height", share: 0.88 };
const STILL_FILL: ViewerFill = { kind: "contain", share: 0.92 };
// The interactive picture's longer side, in device pixels, at most.
const MAX_PICTURE_SIDE = 1024;
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
	itemType( refObjId: number ): number | undefined;
	dressed( gid: number ): boolean;
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
	// The visible 2D canvas the interactive view paints into.
	readonly picture: HTMLCanvasElement;
	readonly search: string;
	// Asks the runtime for frames again after frame() answered "idle".
	readonly wake: () => void;
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

The request's model as a world entity at the origin, facing its native
front: the monster, or a look's body before it is dressed.
================
*/
function viewerEntity( request: ViewerRequest | null ): EntityState {
	const refObjId = request?.kind === "monster" ? request.refObjId : request?.look.bodyRefObjId ?? 0;
	const kind = request?.kind === "look" ? "player" : "monster";
	return { gid: VIEWER_GID, refObjId, kind, regionId: 0, x: 0, y: 0, z: 0, heading: 0, name: "" };
}

/*
================
dressedEntity

The look's body wearing its items, each typed from the catalogue (the
weapon's type word picks the armed idle, actor-motion.ts). Null when the
catalogue lacks an item: the look names something the game does not have.
================
*/
function dressedEntity(
	body: EntityState,
	look: ViewerLook,
	itemType: ( refObjId: number ) => number | undefined
): EntityState | null {
	const equipment: EntityEquipment[] = [], avatars: EntityEquipment[] = [];
	for ( const item of look.worn ) {
		const typeFlags = itemType( item.refItemId );
		if ( typeFlags === undefined ) return null;
		equipment.push( { slot: item.slot, refObjId: item.refItemId, typeFlags, plus: item.plus } );
	}
	for ( const item of look.avatar ) {
		const typeFlags = itemType( item.refItemId );
		if ( typeFlags === undefined ) return null;
		avatars.push( { slot: item.slot, refObjId: item.refItemId, typeFlags, plus: 0 } );
	}
	return { ...body, equipment, avatars };
}

/*
================
ViewerFrame

What frame() asks of the runtime: another frame, none until woken, or
none ever (the owners are disposed).
================
*/
export type ViewerFrame = "draw" | "idle" | "done";

/*
================
zoomed

The framed camera moved along its view line by zoom (1 is the framing).
================
*/
function zoomed( camera: WorldCamera, zoom: number ): WorldCamera {
	const eye = camera.eye.map( ( v, i ) => camera.target[i]! + (v - camera.target[i]!) * zoom );
	return {
		...camera,
		eye: [ eye[0]!, eye[1]!, eye[2]! ],
		near: camera.near! * zoom,
		far: camera.far! * zoom
	};
}

/*
================
dollScene

One full-size doll quad and nothing else.
================
*/
function dollScene( revision: number, width: number, height: number, doll: NonNullable<UiQuad["doll"]> ): UiScene {
	return {
		revision,
		width,
		height,
		quads: [ {
			doll,
			texture: "__doll",
			rect: [ 0, 0, width, height ],
			uv: [ 0, 0, 1, 1 ],
			color: WHITE,
			clip: [ 0, 0, width, height ]
		} ]
	};
}

/*
================
createViewer

Parses the request and returns the per-frame step. frame() answers
whether it wants another frame; once it says no the runtime disposes the
owners. A look whose body or items the catalogue lacks fails
"bad-request" ("unknown-monster" names a monster request's id).
================
*/
export function createViewer( host: ViewerHost ) {
	// settle is the one way out of the still promise: a PNG or a reason.
	let settle: ( outcome: { png: string; } | { reason: ViewerReason; stage: ViewerStage; } ) => void = () => {};
	window.__viewerStill = new Promise<string>( ( resolve, reject ) => {
		settle = outcome =>
			"png" in outcome ?
				resolve( outcome.png ) :
				reject( Error( outcome.reason, { cause: { stage: outcome.stage } } ) );
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
	// The picture's pixels: a still is size x size; the interactive view
	// takes its canvas box's aspect at device pixels, its longer side capped.
	const viewport = { width: request?.size ?? 1, height: request?.size ?? 1 };
	let box: ViewerBounds | null = null;
	// A look spawns its bare body (which starts the catalogue loading) and
	// dresses it once the catalogue can type the items.
	let entity = viewerEntity( request ), entities = [ entity ], dressed = request?.kind !== "look";
	let last = -1, elapsed = 0, revision = 0, settled = 0, reading = false, finished = false;
	let camera: WorldCamera | null = null;
	// The interactive view: the model's turn and the camera's zoom, whether
	// they changed since the last picture, and the drag in progress.
	let yaw = 0, zoom = 1, changed = false, dragX: number | null = null, shown = false;
	// Whether the camera was refit to the first drawn picture (refitCamera).
	let refitted = false;
	/*
	================
	fail
	================
	*/
	const fail = ( reason: ViewerReason ) => {
		if ( finished ) return;
		finished = true;
		const at = stage();
		post( { type: "viewer-error", reason, stage: at } );
		settle( { reason, stage: at } );
	};
	/*
	================
	stage

	How far the draw got, for a failure report: the catalogue still loading,
	the model not yet displayed, the first framed frames, or the readback.
	================
	*/
	const stage = (): ViewerStage => {
		if ( failure ) return "request";
		if ( reading ) return "readback";
		if ( camera ) return "first-frame";
		return host.characters.knows( entity ) === undefined ? "catalogue" : "model";
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
	/*
	================
	paint

	The interactive view's picture: the doll's pixels into the visible canvas.
	================
	*/
	const paint = ( still: { width: number; height: number; rgba: Uint8ClampedArray; } ) => {
		const context = host.picture.getContext( "2d" );
		if ( !context ) return;
		if ( host.picture.width !== still.width ) host.picture.width = still.width;
		if ( host.picture.height !== still.height ) host.picture.height = still.height;
		context.putImageData( new ImageData( new Uint8ClampedArray( still.rgba ), still.width, still.height ), 0, 0 );
	};
	/*
	================
	change

	Input moved the view: draw again (the runtime's loop is idle).
	================
	*/
	const change = () => {
		changed = true;
		settled = SETTLE_FRAMES - 1;
		if ( !reading ) host.wake();
	};
	/*
	================
	measure

	Sizes the render canvas to the picture: the still's square, or the
	interactive canvas's box at device pixels with its longer side capped at
	the request's size limit. A new aspect re-frames the model.
	================
	*/
	function measure() {
		if ( !request?.still ) {
			const width = Math.max( 1, host.picture.clientWidth ) * devicePixelRatio,
				height = Math.max( 1, host.picture.clientHeight ) * devicePixelRatio;
			const scale = Math.min( 1, MAX_PICTURE_SIDE / Math.max( width, height ) );
			viewport.width = Math.max( 1, Math.round( width * scale ) );
			viewport.height = Math.max( 1, Math.round( height * scale ) );
		}
		host.canvas.width = viewport.width;
		host.canvas.height = viewport.height;
		if ( box ) {
			camera = viewerCamera( box, MODEL_FRONT, viewport.width / viewport.height );
			refitted = false;
		}
	}
	if ( failure ) fail( failure );
	else if ( !request?.still ) {
		window.addEventListener( "resize", () => {
			measure();
			change();
		} );
		host.picture.addEventListener( "pointerdown", event => {
			dragX = event.clientX;
			host.picture.setPointerCapture( event.pointerId );
		} );
		host.picture.addEventListener( "pointermove", event => {
			if ( dragX === null || !camera ) return;
			yaw += (event.clientX - dragX) * YAW_PER_PIXEL;
			dragX = event.clientX;
			change();
		} );
		// A cancelled touch or a lost capture ends the drag as a release does,
		// so the next move does not turn the model.
		for ( const end of [ "pointerup", "pointercancel", "lostpointercapture" ] as const ) {
			host.picture.addEventListener( end, () => {
				dragX = null;
			} );
		}
		host.picture.addEventListener( "wheel", event => {
			if ( !camera ) return;
			event.preventDefault();
			zoom = Math.min( MAX_ZOOM, Math.max( MIN_ZOOM, zoom * Math.exp( event.deltaY * ZOOM_PER_WHEEL_PIXEL ) ) );
			change();
		}, { passive: false } );
	}
	if ( !failure ) {
		measure();
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
		frame( now: number ): ViewerFrame {
			if ( finished && !shown ) return "done";
			elapsed += last < 0 ? 0 : Math.min( Math.max( now - last, 0 ), MAX_FRAME_STEP_MS );
			last = now;
			if ( !finished && elapsed > VIEWER_TIMEOUT_MS ) {
				fail( "timeout" );
				return "done";
			}
			// A still keeps the loop until it is settled; the interactive view
			// waits for its readback, which wakes it on a change.
			if ( reading ) return request?.still ? "draw" : "idle";
			if ( host.renderer.phase() === "failed" ) {
				fail( "render" );
				return "done";
			}
			const known = host.characters.knows( entity );
			if ( known === false ) {
				fail( request?.kind === "look" ? "bad-request" : "unknown-monster" );
				return "done";
			}
			if ( !dressed && known && request?.kind === "look" ) {
				const worn = dressedEntity( entity, request.look, refObjId => host.characters.itemType( refObjId ) );
				if ( !worn ) {
					fail( "bad-request" );
					return "done";
				}
				entity = worn;
				entities = [ entity ];
				dressed = true;
			}
			if ( host.characters.error() ) {
				fail( "asset-load" );
				return "done";
			}
			const seconds = elapsed / 1000;
			host.characters.step( entities, null, seconds );
			if ( !camera && dressed && host.characters.ready( VIEWER_GID ) && host.characters.dressed( VIEWER_GID ) ) {
				const bounds = host.renderer.characterBounds?.( VIEWER_GID );
				// The doll turns the model by pi about y (portrait.ts previewYaw of
				// yaw 0), so model x and z change sign in the doll's space.
				if ( bounds ) {
					box = { min: [ -bounds[3], bounds[1], -bounds[5] ], max: [ -bounds[0], bounds[4], -bounds[2] ] };
					camera = viewerCamera( box, MODEL_FRONT, viewport.width / viewport.height );
				}
			}
			if ( camera ) {
				host.renderer.setUi(
					dollScene( ++revision, viewport.width, viewport.height, {
						gid: VIEWER_GID,
						yaw,
						camera: zoomed( camera, zoom )
					} )
				);
			}
			host.renderer.frame( viewport, seconds );
			if ( !camera || ++settled < SETTLE_FRAMES ) return "draw";
			changed = false;
			reading = true;
			host.renderer.readDoll!().then( still => {
				// The first picture only measures: frame what was drawn, draw again.
				const drawn = refitted ? null : opaqueBox( still.rgba, still.width, still.height );
				refitted = true;
				if ( drawn && camera ) {
					camera = refitCamera(
						camera,
						drawn,
						still.width,
						still.height,
						request?.still ? STILL_FILL : VIEW_FILL
					);
					reading = false;
					settled = SETTLE_FRAMES - 1;
					host.wake();
					return;
				}
				// A still is read once: it stays reading until the PNG settles it.
				if ( request?.still ) return encodePng( still ).then( succeed );
				reading = false;
				paint( still );
				if ( !shown ) {
					// The first picture: viewer-ready, and __viewerStill holds it.
					shown = true;
					void encodePng( still ).then( succeed );
				}
				// A change while the readback ran draws again.
				if ( changed ) host.wake();
			} ).catch( () => fail( "render" ) );
			return request?.still ? "draw" : "idle";
		}
	};
}
