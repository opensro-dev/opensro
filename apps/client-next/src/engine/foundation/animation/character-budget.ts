/*
===========================================================================

character-budget.ts - residency and structure limits for character sources

Every decoded character source (bodies, worn items, effect programs) is
charged against these limits before the renderer admits it. The byte
budgets bound memory; the structural limits reject malformed sources.

===========================================================================
*/
import type { CharacterModel } from "@/engine/contracts/character";
import { DECODED_IMAGE_BYTES, imageBytes } from "@/engine/foundation/assets/image-budget";
// Retail colored fireworks contain 100 parent particles with 50 children each.
export const CHARACTER_PARTICLE_BIRTHS = 8192;
export const CHARACTER_PRIMITIVES = 1024;
export const CHARACTER_IMAGES = 64;
// A count guard only: CHARACTER_RESIDENT_BYTES bounds residency. Native
// refcounts item resources with no count limit, and every worn item is its
// own source (CCObjCharacter_SetEquipSlotVisual), so the guard is sized so
// the byte budget binds first. Measured over all 1714 published character
// sources (2026-09-30), a source is charged 1.04 MB at the median and
// 1.36 MB on average, so 256 MiB holds about 200-260 of them. 64 could not
// hold one creation wardrobe (72 Europe sources).
export const CHARACTER_MODELS = 1024;
// Borrowed and assembled views (an equipped actor's merged body), which
// share their sources' bytes and so need their own count bound.
export const CHARACTER_ASSEMBLIES = 1024;
export const CHARACTER_ACTORS = 512;
export const CHARACTER_MODEL_BYTES = 67108864;
export const CHARACTER_RESIDENT_BYTES = 268435456;
// One admitted source, geometry plus decoded pixels. Admission reserves this
// class for a source it has never decoded, so the class must bound the decode
// rather than the decoder's structural guards: the largest published character
// source is 5.1 MiB (npc/mob/asiam/ivy.glb) and the largest effect program is
// 2.7 MiB (monster/rm_tahomet_spell_ready.efp).
export const CHARACTER_SOURCE_BYTES = 16777216;
// Additional renderer storage, separate from decoded source residency. Includes
// CPU poses/palettes and GPU copies, padded instances and expanded draw geometry.
export const CHARACTER_RENDER_BYTES = 67108864;
/*
================
characterPoseBytes
================
*/
export function characterPoseBytes( model: CharacterModel ): number {
	// Local/global matrices, mutable and retained rest TRS, blend scratch,
	// weights, sample and one exact palette per primitive/pose revision.
	return model.nodes.length * ((16 + 16 + 3 + 4 + 3 + 10 + 10 + 6) * 4 + 2) + 16 +
		model.primitives.reduce( ( bytes, primitive ) => bytes + primitive.joints.length * 64, 0 ) +
		model.clips.reduce( ( bytes, clip ) => bytes + clip.channels.length * 80, 0 ) +
		model.clips.reduce( ( max, clip ) => Math.max( max, clip.channels.length ), 0 ) * 32;
}
/*
================
characterMaterialClockBytes
================
*/
export function characterMaterialClockBytes( model: CharacterModel ): number {
	return model.primitives.reduce( ( sum, p ) => {
		const m = p.geometry.material;
		return sum + (p.equipmentGlow ? 128 : 0) + (p.modifierSource ?
			[ ...p.modifierSource.modifiers.materialModifiers, ...p.modifierSource.modifiers.textureModifiers ].reduce(
				( n, row ) => n + 128 + ("colors" in row ? row.colors.length * 40 : 0),
				0
			) :
			0) +
			(m?.colorTimeline ? 128 + m.colorTimeline.colors.length * 40 : 0) + (m?.uvVelocity || m?.uvAtlas ? 128 : 0);
	}, 0 );
}
/*
================
characterBatchBytes
================
*/
export function characterBatchBytes( model: CharacterModel, count: number ): number {
	if ( !count ) return 0;
	// Ordinary mesh batches retain a power-of-two capacity across visibility
	// changes. Charge CPU palettes and GPU bones for that capacity too.
	if ( !model.primitives.some( p => p.emission || p.ribbon ) ) count = 2 ** Math.ceil( Math.log2( count ) );
	// Reserve bounded pose indices/revisions and per-binding offsets as well.
	// Palette storage below deliberately charges the unshared worst case.
	return count * (72 + 64) + model.primitives.reduce( ( bytes, primitive ) => {
		const geometry = primitive.geometry,
			instances = count * (primitive.emission?.capacity ?? primitive.emission?.births.length ?? 1),
			slots = 2 ** Math.ceil( Math.log2( Math.max( 1, instances ) ) );
		return bytes + count * 24 + (primitive.ribbon ?
			count *
			Math.max( 2, 3 * ((primitive.emission?.capacity ?? primitive.emission?.births.length ?? 1) - 1) + 1 ) * 2 *
			160 :
			0) +
			instances * primitive.joints.length * 64 * 2 + slots * 112 + instances * 96 +
			geometry.positions.length / 3 * 14 * 4 * 2 + geometry.indices.byteLength +
			(geometry.joints?.length ?? 0) * 8 * 2 + 64 + 96 + 20;
	}, 0 );
}
/*
================
characterBytes

The resident bytes of one decoded source; throws when its structure or
decode exceeds the limits.
================
*/
export function characterBytes( model: CharacterModel, images: readonly { width: number; height: number; }[] ): number {
	if (
		model.nodes.length > 1024 || model.primitives.length > CHARACTER_PRIMITIVES ||
		model.images.length > CHARACTER_IMAGES || images.length !== model.images.length
	) {
		throw new Error( "Character structure exceeds budget" );
	}
	let graphBytes = 0;
	for ( const [index, e] of (model.particleGraph ?? []).entries() ) {
		if (
			!Number.isInteger( e.parent ) || e.parent >= index || e.parent < -1 ||
			e.births.length > CHARACTER_PARTICLE_BIRTHS || e.parents.length !== e.births.length || e.births.some( b =>
				!Number.isSafeInteger( b ) || b < 0
			) || e.parents.some( b =>
				!Number.isSafeInteger( b ) || b < 0 ||
				e.parent >= 0 && b >= model.particleGraph![e.parent]!.births.length
			) || !Number.isInteger( e.frames ) || e.frames < 0 || e.frames > 1200
		) throw Error( "Invalid particle graph" );
		if (
			e.capacity !== undefined &&
			(!Number.isSafeInteger( e.capacity ) || e.capacity < 0 || e.capacity > CHARACTER_PARTICLE_BIRTHS)
		) throw Error( "Invalid particle graph capacity" );
		graphBytes += e.births.length * 16 +
			(e.scales.length * 3 + e.positions.length * 3 + e.rotations.length * 16) * 8 + 256;
	}
	const buffers = new Set<ArrayBufferLike>();
	for ( const p of model.primitives ) {
		if (
			p.emission?.capacity !== undefined &&
			(!Number.isSafeInteger( p.emission.capacity ) || p.emission.capacity < 0 ||
				p.emission.capacity > CHARACTER_PARTICLE_BIRTHS)
		) throw Error( "Invalid particle capacity" );
		if (
			p.emission &&
			(!Array.isArray( p.emission.births ) || p.emission.births.length > CHARACTER_PARTICLE_BIRTHS ||
				p.emission.births.some( t => !Number.isFinite( t ) || t < 0 ) ||
				!Number.isFinite( p.emission.lifetime ) || p.emission.lifetime <= 0 || p.emission.lifetime > 30)
		) throw new Error( "Invalid particle schedule" );
		if ( p.materialFrames ) {
			const frames = p.materialFrames;
			if (
				!Number.isFinite( frames.fps ) || frames.fps <= 0 || !frames.colors.length ||
				frames.colors.length % 4 || frames.windows.length !== frames.colors.length ||
				!frames.colors.every( Number.isFinite ) || !frames.windows.every( Number.isFinite )
			) throw new Error( "Invalid effect material frames" );
		}
		if ( p.ribbon ) buffers.add( p.ribbon.widths.buffer );
		buffers.add( p.inverseBind.buffer );
		if ( p.materialFrames ) {
			buffers.add( p.materialFrames.colors.buffer );
			buffers.add( p.materialFrames.windows.buffer );
		}
		for ( const value of Object.values( p.geometry ) ) {
			if ( ArrayBuffer.isView( value ) ) buffers.add( value.buffer );
		}
	}
	for ( const clip of model.clips ) {
		for ( const channel of clip.channels ) {
			buffers.add( channel.times.buffer );
			buffers.add( channel.values.buffer );
		}
	}
	for ( const image of model.images ) buffers.add( image.bytes.buffer );
	const bytes = characterMaterialClockBytes( model ) + graphBytes +
		model.primitives.reduce( ( sum, p ) => sum + (p.emission?.births.length ?? 0) * 8, 0 ) +
		[ ...buffers ].reduce( ( sum, buffer ) => sum + buffer.byteLength, 0 );
	const pixels = images.reduce( ( sum, image ) => sum + imageBytes( image.width, image.height ), 0 );
	const total = bytes + pixels + images.reduce( ( sum, image ) => sum + image.width * image.height, 0 );
	if ( bytes > CHARACTER_MODEL_BYTES || pixels > DECODED_IMAGE_BYTES || total > CHARACTER_SOURCE_BYTES ) {
		throw new Error( "Character decoded bytes exceed budget" );
	}
	return total;
}
