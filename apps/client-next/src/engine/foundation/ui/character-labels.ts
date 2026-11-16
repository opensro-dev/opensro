/*
===========================================================================

character-labels.ts - place the interface's anchored quads for this frame

The UI owns native glyph extents; the renderer owns the current frame's
model and camera anchors. Each frame this turns anchored quads (names and
speech on characters, world-anchored text) into screen quads with a depth.
A missing model or a retired actor removes its whole anchored product.

A projection instance keeps last frame's placements: a label whose placed
values did not change is the same object again, and a frame where nothing
changed is the same scene again, so the UI device skips its records (it
trusts quad and scene identity, device/ui.ts prepare).

===========================================================================
*/
import type { UiQuad, UiScene } from "@/engine/contracts/ui";
import { regionRelativeXZ } from "@/engine/foundation/world/world-space";

// CICharactor_RenderOverheadNameAndIndicators (85F4A0): the board stands 2
// above the character's height, or 7 above its ride's.
const LABEL_LIFT = 2;
const RIDE_LABEL_LIFT = 7;
// CIItem labels stand five units above the ground (CIGIDObject height).
const GROUND_ITEM_LABEL_HEIGHT = 5;

/*
================
characterLabelHeight

How far above its feet a character's name board is anchored. 85F4A0 adds
the lift to CICharactor +0x224, the height that eases toward +0x220 =
GetCompositeScale (characterInfo height x 0.5, record +0x08, times the
actor's scale) x 20: the published height, never the model's bounds, so a
quadruped whose rest pose is low still carries its name above it. bindTop
serves only an actor the catalogue gives no height (not a CICharactor).
================
*/
export function characterLabelHeight(
	body: {
		readonly height?: number;
		readonly scale: number;
		readonly groundItem?: boolean;
	},
	bindTop: number,
	riding: boolean
): number {
	if ( body.groundItem ) return GROUND_ITEM_LABEL_HEIGHT;
	const lift = riding ? RIDE_LABEL_LIFT : LABEL_LIFT;
	return (body.height ?? bindTop) * body.scale + lift;
}

/*
================
retainedPlacement

The placed quad kept for q when it has exactly these placed values. A
placed quad is q's fields with the rect's origin and the depth replaced, so
equal x, y and depth (Object.is: -0 and 0 pack different bytes) mean an
equal quad. Without placements (live quads) nothing is retained.
================
*/
function retainedPlacement(
	placements: WeakMap<UiQuad, UiQuad> | null,
	q: UiQuad,
	x: number,
	y: number,
	depth: number
): UiQuad | undefined {
	const kept = placements?.get( q );
	return kept && Object.is( kept.rect[0], x ) && Object.is( kept.rect[1], y ) && Object.is( kept.depth, depth ) ?
		kept :
		undefined;
}

/*
================
projectWorldAnchor

A world-anchored quad on screen, or null behind the eye or outside the
depth range. Its rect is an offset from the projected anchor.
================
*/
function projectWorldAnchor(
	q: UiQuad,
	scene: UiScene,
	world: { origin: number; matrix: Float32Array; },
	placements: WeakMap<UiQuad, UiQuad> | null
): UiQuad | null {
	const a = q.worldAnchor!, m = world.matrix;
	const [x, z] = regionRelativeXZ( world.origin, a ),
		y = a.y;
	const w = m[3]! * x + m[7]! * y + m[11]! * z + m[15]!;
	if ( w <= 0 ) return null;
	const px = (1 + (m[0]! * x + m[4]! * y + m[8]! * z + m[12]!) / w) * scene.width / 2,
		py = (1 - (m[1]! * x + m[5]! * y + m[9]! * z + m[13]!) / w) * scene.height / 2;
	const depth = (m[2]! * x + m[6]! * y + m[10]! * z + m[14]!) / w;
	if ( depth < 0 || depth > 1 ) return null;
	const left = px + q.rect[0], top = py + q.rect[1];
	const kept = retainedPlacement( placements, q, left, top, depth );
	if ( kept ) return kept;
	const { worldAnchor, ...quad } = q;
	const placed = { ...quad, depth, rect: [ left, top, q.rect[2], q.rect[3] ] as const };
	placements?.set( q, placed );
	return placed;
}

/*
================
projectCharacterAnchor

A character-anchored quad at its anchor: translated by the anchor's whole
pixel, so text layout stays translation-invariant, with the anchor's depth.
================
*/
function projectCharacterAnchor(
	q: UiQuad,
	anchor: readonly [number, number, number],
	placements: WeakMap<UiQuad, UiQuad>
): UiQuad {
	const left = Math.trunc( anchor[0] ) + q.rect[0], top = Math.trunc( anchor[1] ) + q.rect[1];
	const kept = retainedPlacement( placements, q, left, top, anchor[2] );
	if ( kept ) return kept;
	const { characterAnchor, ...quad } = q;
	const placed = { ...quad, depth: anchor[2], rect: [ left, top, q.rect[2], q.rect[3] ] as const };
	placements.set( q, placed );
	return placed;
}

/*
================
createLabelProjection

One renderer owns one projection. Placements are keyed by the admitted
source quad (admission keeps an equal quad's object, ui.ts copyUiProduct)
and reused only when their placed values are equal, so the output equals a
fresh projection's quad for quad. The previous scene object comes back
only for the same source scene with every output quad identical: the scene
is the source's fields plus those quads, so nothing else can differ. Live
quads (damage text) are new objects every frame and are never retained.
================
*/
export function createLabelProjection() {
	const placements = new WeakMap<UiQuad, UiQuad>();
	// One sort row per anchored quad of a frame, reused across frames.
	const rows: { depth: number; order: number; quad: UiQuad | null; }[] = [];
	const characterRows: typeof rows = [];
	let lastSource: UiScene | null = null, lastScene: UiScene | null = null;
	let quads: UiQuad[] = [];
	return {
		/*
		================
		project

		The scene with its anchored quads placed. live quads (the frame's
		damage text) follow the scene's own, after the labels they cover;
		without world they are dropped like the scene's world-anchored quads.
		================
		*/
		project(
			scene: UiScene,
			anchors: ReadonlyMap<number, readonly [number, number, number]>,
			world?: { origin: number; matrix: Float32Array; },
			live: readonly UiQuad[] = []
		): UiScene {
			// A frame that threw part way leaves nothing behind.
			quads.length = 0;
			characterRows.length = 0;
			let order = 0;
			const flushCharacters = () => {
				if ( !characterRows.length ) return;
				characterRows.sort( ( a, b ) => b.depth - a.depth || a.order - b.order );
				for ( const row of characterRows ) {
					quads.push( row.quad! );
					row.quad = null;
				}
				characterRows.length = 0;
			};
			for ( const q of scene.quads ) {
				if ( q.characterAnchor !== undefined ) {
					const anchor = anchors.get( q.characterAnchor );
					if ( !anchor ) continue;
					const row = rows[order] ??= { depth: 0, order: 0, quad: null };
					row.depth = anchor[2];
					row.order = order++;
					row.quad = projectCharacterAnchor( q, anchor, placements );
					characterRows.push( row );
					continue;
				}
				flushCharacters();
				if ( q.worldAnchor ) {
					const placed = world ? projectWorldAnchor( q, scene, world, placements ) : null;
					if ( placed ) quads.push( placed );
					continue;
				}
				quads.push( q );
			}
			flushCharacters();
			for ( const q of live ) {
				const placed = world && q.worldAnchor ? projectWorldAnchor( q, scene, world, null ) : null;
				if ( placed ) quads.push( placed );
			}
			const previous = lastScene?.quads;
			let same = scene === lastSource && previous !== undefined && previous.length === quads.length;
			for ( let i = 0; same && i < quads.length; i++ ) same = quads[i] === previous![i];
			if ( same ) return lastScene!;
			lastSource = scene;
			lastScene = { ...scene, quads };
			quads = [];
			return lastScene;
		}
	};
}

/*
================
projectCharacterLabels

One frame's projection with nothing retained: what every frame of a
projection instance must equal.
================
*/
export function projectCharacterLabels(
	scene: UiScene,
	anchors: ReadonlyMap<number, readonly [number, number, number]>,
	world?: { origin: number; matrix: Float32Array; },
	live: readonly UiQuad[] = []
): UiScene {
	return createLabelProjection().project( scene, anchors, world, live );
}
