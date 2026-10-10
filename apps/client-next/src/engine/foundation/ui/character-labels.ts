/*
===========================================================================

character-labels.ts - place the interface's anchored quads for this frame

The UI owns native glyph extents; the renderer owns the current frame's
model and camera anchors. Each frame this turns anchored quads (names and
speech on characters, world-anchored text) into screen quads with a depth.
A missing model or a retired actor removes its whole anchored product.

===========================================================================
*/
import type { UiQuad, UiScene } from "@/engine/contracts/ui";

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
projectWorldAnchor

A world-anchored quad on screen, or null behind the eye or outside the
depth range. Its rect is an offset from the projected anchor.
================
*/
function projectWorldAnchor(
	q: UiQuad,
	scene: UiScene,
	world: { origin: number; matrix: Float32Array; }
): UiQuad | null {
	const a = q.worldAnchor!, m = world.matrix;
	const x = a.x + ((a.regionId & 255) - (world.origin & 255)) * 1920,
		y = a.y,
		z = a.z + ((a.regionId >>> 8) - (world.origin >>> 8)) * 1920;
	const w = m[3]! * x + m[7]! * y + m[11]! * z + m[15]!;
	if ( w <= 0 ) return null;
	const px = (1 + (m[0]! * x + m[4]! * y + m[8]! * z + m[12]!) / w) * scene.width / 2,
		py = (1 - (m[1]! * x + m[5]! * y + m[9]! * z + m[13]!) / w) * scene.height / 2;
	const depth = (m[2]! * x + m[6]! * y + m[10]! * z + m[14]!) / w;
	if ( depth < 0 || depth > 1 ) return null;
	const { worldAnchor, ...quad } = q;
	return { ...quad, depth, rect: [ px + q.rect[0], py + q.rect[1], q.rect[2], q.rect[3] ] as const };
}

/*
================
projectCharacterLabels

The scene with its anchored quads placed. live quads (the frame's damage
text) follow the scene's own, after the labels they cover; without world
they are dropped like the scene's world-anchored quads.
================
*/
export function projectCharacterLabels(
	scene: UiScene,
	anchors: ReadonlyMap<number, readonly [number, number, number]>,
	world?: { origin: number; matrix: Float32Array; },
	live: readonly UiQuad[] = []
): UiScene {
	const quads: UiQuad[] = [];
	for ( const q of scene.quads ) {
		if ( q.worldAnchor ) {
			const placed = world ? projectWorldAnchor( q, scene, world ) : null;
			if ( placed ) quads.push( placed );
			continue;
		}
		if ( q.characterAnchor === undefined ) {
			quads.push( q );
			continue;
		}
		const anchor = anchors.get( q.characterAnchor );
		if ( !anchor ) continue;
		const { characterAnchor, ...quad } = q;
		quads.push( {
			...quad,
			depth: anchor[2],
			rect: [ Math.trunc( anchor[0] ) + q.rect[0], Math.trunc( anchor[1] ) + q.rect[1], q.rect[2], q.rect[3] ]
		} );
	}
	for ( const q of live ) {
		const placed = world && q.worldAnchor ? projectWorldAnchor( q, scene, world ) : null;
		if ( placed ) quads.push( placed );
	}
	return { ...scene, quads };
}
