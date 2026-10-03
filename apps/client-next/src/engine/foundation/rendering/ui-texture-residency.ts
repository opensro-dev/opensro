/*
===========================================================================

ui-texture-residency.ts - which UI textures the GPU must hold

Preloading is CPU/cache readiness, not GPU residency. GPU demand derives
from the committed draw product (including masks), never all known assets.
The renderer owns uploads and releases and clears its resident set on
device loss. This runs on every UI republish, so it allocates only its
answer.

===========================================================================
*/
import type { UiScene } from "@/engine/contracts/ui";

/*
================
uiTextureResidency

needed: textures the scene draws that the renderer has. release: resident
ones no longer needed. upload: needed ones not resident, or dirty.
available is the renderer's texture map, keyed by texture id. live are
the textures of what the renderer draws for the scene each frame (its
damage text), needed as long as the scene is.
================
*/
export function uiTextureResidency(
	scene: UiScene | null,
	available: ReadonlyMap<string, unknown>,
	resident: ReadonlySet<string>,
	dirty: ReadonlySet<string>,
	live: readonly string[] = []
) {
	const needed = new Set<string>();
	for ( const id of scene ? live : [] ) if ( available.has( id ) ) needed.add( id );
	for ( const quad of scene?.quads ?? [] ) {
		if ( quad.texture && available.has( quad.texture ) ) needed.add( quad.texture );
		const mask = quad.mask?.texture;
		if ( mask && available.has( mask ) ) needed.add( mask );
	}
	const release: string[] = [], upload: string[] = [];
	for ( const id of resident ) if ( !needed.has( id ) ) release.push( id );
	for ( const id of needed ) if ( !resident.has( id ) || dirty.has( id ) ) upload.push( id );
	return { release, upload, needed };
}
