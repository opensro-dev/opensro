/*
===========================================================================

character-budget.ts - residency and structure limits for character sources

Every decoded character source (bodies, worn items, effect programs) is
charged against these limits before the renderer admits it. The byte
budgets bound memory; the structural limits reject malformed sources.

===========================================================================
*/
import { clothData, clothBytes, type ClothData } from "./cloth";
import {
	clipTimelineBytes,
	createClipTimelineCache,
	type ClipNodeChannelCache,
	type ClipTimelineCache
} from "./animation-timelines";
import { paletteBindings } from "./palette-bindings";
import { CLOTH_STREAM_WORDS, clothStreamJoints, clothStreamSlots } from "./cloth-stream";
import type { CharacterModel } from "@/engine/contracts/character";
import type { NativeTexture } from "@/engine/contracts/texture";
import { validateNativeTexture } from "@/engine/foundation/assets/native-texture";
import { DECODED_IMAGE_BYTES, imageBytes } from "@/engine/foundation/assets/image-budget";
import { ROW_STATE_FLOATS } from "@/engine/foundation/rendering/geometry-instances";
// Retail colored fireworks contain 100 parent particles with 50 children each.
export const CHARACTER_PARTICLE_BIRTHS = 8192;
export const CHARACTER_PRIMITIVES = 1024;
// The node graph a decoded model may carry. An effect program compiles into
// one, and the native EFP tree has no node count of its own: B29EF8 reads a
// u32 child count and B29F10..B29F33 recurses with no bound.
export const CHARACTER_NODES = 1024;
export const CHARACTER_IMAGES = 64;
// A structural count guard; the byte budget governs retained sources. Native
// equipment shares refcounted resources rather than copying each actor's items.
// With native mip resources, the 1714-source catalog has a 212768-byte median
// and a 429613-byte mean charge (2026-09-30). A dense population still requires
// scene measurement: the catalog is not one simultaneously resident scene.
export const CHARACTER_MODELS = 1024;
// Borrowed and assembled views (an equipped actor's merged body), which
// share their sources' bytes and so need their own count bound.
export const CHARACTER_ASSEMBLIES = 1024;
export const CHARACTER_ACTORS = 512;
export const CHARACTER_MODEL_BYTES = 67108864;
export const CHARACTER_RESIDENT_BYTES = 268435456;
// One admitted source, geometry plus retained bitmap pixels or native mip blocks. Admission reserves this
// class for a source it has never decoded, so the class must bound the decode
// rather than the decoder's structural guards. The 2026-09-30 native-texture
// census measured the largest character source at 3242004 bytes
// (npc/mob/karakoram/isyutaru.glb); keep headroom for future source revisions.
export const CHARACTER_SOURCE_BYTES = 16777216;
// Additional renderer storage, separate from decoded source residency. Includes
// CPU poses/palettes and GPU copies, padded instances and expanded draw geometry.
export const CHARACTER_RENDER_BYTES = 67108864;
// A pose's entries for each clip of its model: the name map and the
// sampling eligibility sets (animation-pose.ts registerClip).
export const POSE_CLIP_ENTRY_BYTES = 96;
/*
================
CharacterPlanCaches

What a render plan derives from a model's immutable parts, owned by the
character renderer: per-clip timeline charges and channel-by-node indexes
(every pose of the renderer shares the clip's one), per-primitive-list
palette bindings, and per-cloth-piece stream slots and the palette entries
they read. Admitting a clip keeps a model's primitive list, and with it
each primitive's cloth data and mesh, which an assembly shares with every
other wearer of the part (characters.ts assembly spreads the part's
primitive), so a re-plan of every assembly of the body computes none of
them again. The indexes are a few words a channel, slot or entry, held
once for the renderer, not per pose or per plan.
================
*/
export type CharacterPlanCaches = {
	readonly timelines: ClipTimelineCache;
	readonly nodeChannels: ClipNodeChannelCache;
	readonly bindings: WeakMap<CharacterModel["primitives"], ReturnType<typeof paletteBindings>>;
	readonly clothSlots: WeakMap<ClothData, Int32Array>;
	// With the mesh they were read from: the entries depend on it too.
	readonly clothJoints: WeakMap<
		ClothData,
		{ readonly mesh: CharacterModel["primitives"][number]["geometry"]; readonly joints: Int32Array; }
	>;
};
/*
================
createCharacterPlanCaches
================
*/
export function createCharacterPlanCaches(): CharacterPlanCaches {
	return {
		timelines: createClipTimelineCache(),
		nodeChannels: new WeakMap(),
		bindings: new WeakMap(),
		clothSlots: new WeakMap(),
		clothJoints: new WeakMap()
	};
}
/*
================
characterClothSlots

A cloth piece's stream slots (cloth-stream.ts clothStreamSlots), computed
once per cloth data when the plan caches are given. Every wearer's stream
and its draw's slot map take the same list; nobody writes it.
================
*/
export function characterClothSlots( cloth: ClothData, caches?: CharacterPlanCaches ): Int32Array {
	let slots = caches?.clothSlots.get( cloth );
	if ( !slots ) {
		slots = clothStreamSlots( cloth );
		caches?.clothSlots.set( cloth, slots );
	}
	return slots;
}
/*
================
characterClothJoints

The palette entries a skinned cloth piece's stream reads (cloth-stream.ts
clothStreamJoints), computed once per cloth data when the plan caches are
given: a piece's cloth data and mesh come from one decoded primitive. The
entries are kept with that mesh, so a cloth data met with another mesh is
read again, not trusted.
================
*/
export function characterClothJoints(
	primitive: Pick<CharacterModel["primitives"][number], "cloth" | "geometry">,
	caches?: CharacterPlanCaches
): Int32Array {
	let read = caches?.clothJoints.get( primitive.cloth! );
	if ( !read || read.mesh !== primitive.geometry ) {
		read = {
			mesh: primitive.geometry,
			joints: clothStreamJoints( primitive.geometry, characterClothSlots( primitive.cloth!, caches ) )
		};
		caches?.clothJoints.set( primitive.cloth!, read );
	}
	return read.joints;
}
/*
================
characterPoseBytes

Charge pose evaluation storage separately from immutable source data. A
clip costs a pose its entries and, once sampled, one cursor per timeline
(animation-timelines.ts), not one per channel: a native clip keys all its
channels at the same times. A pose keeps one palette per distinct binding
(palette-bindings.ts), however many primitives draw from it: a body's
parts on one skin share one. The renderer passes its plan caches.
================
*/
export function characterPoseBytes( model: CharacterModel, caches?: CharacterPlanCaches ): number {
	let bindings = caches?.bindings.get( model.primitives );
	if ( !bindings ) {
		bindings = paletteBindings( model );
		caches?.bindings.set( model.primitives, bindings );
	}
	// Local/global and attachment bind matrices, mutable and retained rest TRS, blend scratch,
	// weights, socket chain scratch, sample, per-node flags and one exact palette per
	// binding/pose revision.
	return model.nodes.length * ((16 + 16 + 16 + 3 + 4 + 3 + 10 + 10 + 6 + 1) * 4 + 3) + 16 +
		model.primitives.reduce(
			( bytes, primitive ) =>
				bytes + ((bindings.get( primitive ) ?? primitive) === primitive ? primitive.joints.length * 64 : 0),
			0
		) +
		model.clips.reduce(
			( bytes, clip ) => bytes + POSE_CLIP_ENTRY_BYTES + clipTimelineBytes( clip, caches?.timelines ),
			0
		) +
		// The quaternion bracket scratch, sized by the largest clip.
		model.clips.reduce( ( max, clip ) => Math.max( max, clip.channels.length ), 0 ) * 32;
}
/*
================
characterMaterialClockBytes

Reserve per-material modifier clocks and authored timeline storage.
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
			(m?.colorTimeline ? 128 + m.colorTimeline.colors.length * 40 : 0) +
			(m?.uvVelocity || m?.uvAtlas ? 128 : 0) +
			(m?.textureFactorPulse ? 64 : 0);
	}, 0 );
}
/*
================
characterSharedMesh

Whether a primitive draws from a mesh the device keeps one GPU copy of,
shared by every batch that draws it (device/geometry.ts sharedMesh): a
skinned primitive that is not a ribbon. A skinned cloth piece shares it
too: its solver writes only the stream in the piece's own skin table
(cloth-stream.ts), never the vertices. Ribbons and unskinned cloth rewrite
their vertices each frame, so each batch keeps its own stream.
================
*/
export function characterSharedMesh( primitive: CharacterModel["primitives"][number] ): boolean {
	return !primitive.ribbon && !!primitive.geometry.joints && !!primitive.geometry.weights;
}
/*
================
characterRowGlow

An equipment glow on a primitive whose instances are its batch's actor
rows (not particle slots or ribbon strips): a batch of several rows keeps
each row's glow colour and UV in a row-state table (characters.ts).
================
*/
export function characterRowGlow( primitive: CharacterModel["primitives"][number] ): boolean {
	return !!primitive.equipmentGlow && !primitive.emission && !primitive.ribbon;
}
/*
================
characterMeshBytes

The one GPU copy of a shared mesh: its packed vertex stream (14 floats a
vertex), its indices and its skin table (four joints and four weights a
vertex).
================
*/
export function characterMeshBytes( geometry: CharacterModel["primitives"][number]["geometry"] ): number {
	return geometry.positions.length / 3 * 14 * 4 + geometry.indices.byteLength + (geometry.joints?.length ?? 0) * 8;
}
/*
================
characterSharedPalette

Whether a model's batches keep their palettes as shared streams
(palette-streams.ts): every primitive skinned, none an emitter, a ribbon
or a billboard.
================
*/
export function characterSharedPalette( model: CharacterModel ): boolean {
	return !model.primitives.some( p =>
		p.emission || p.ribbon || p.billboard || !p.geometry.joints || !p.geometry.weights
	);
}
/*
================
characterBatchBytes

Charge retained batch capacity rather than only this frame's visible
population. A shared mesh is charged once per frame plan, not here
(characterMeshBytes); a batch's own streams (ribbons, unskinned cloth and
meshes) keep a GPU copy and a CPU mirror each. The renderer passes its
plan caches.
================
*/
export function characterBatchBytes( model: CharacterModel, count: number, caches?: CharacterPlanCaches ): number {
	if ( !count ) return 0;
	// Mesh and particle batches retain capacity across membership changes.
	// Reserve unused particle slots too; ribbons retain exact-sized strips.
	if ( !model.primitives.some( p => p.ribbon ) ) count = 2 ** Math.ceil( Math.log2( count ) );
	// A shared-palette batch keeps one palette stream per distinct binding
	// (palette-streams.ts), its CPU data and its GPU copy, however many
	// primitives draw from it: a body's parts share one. Every other batch
	// keeps a palette per primitive.
	const bindings = characterSharedPalette( model ) ? paletteBindings( model ) : undefined;
	const streams = new Set<CharacterModel["primitives"][number]>();
	/*
	================
	palettes

	The palette bytes primitive adds: its stream's on the first primitive
	of a shared binding, none on the others, its own otherwise.
	================
	*/
	const palettes = ( primitive: CharacterModel["primitives"][number], instances: number ) => {
		if ( !bindings ) return instances * primitive.joints.length * 64 * 2;
		const stream = bindings.get( primitive ) ?? primitive;
		if ( streams.has( stream ) ) return 0;
		streams.add( stream );
		return count * stream.joints.length * 64 * 2;
	};
	// Reserve bounded pose indices/revisions and per-binding offsets as well.
	return count * (72 + 64) + model.primitives.reduce( ( bytes, primitive ) => {
		const geometry = primitive.geometry,
			instances = count * (primitive.emission?.capacity ?? primitive.emission?.births.length ?? 1),
			slots = 2 ** Math.ceil( Math.log2( Math.max( 1, instances ) ) );
		return bytes + (primitive.cloth ? count * geometry.positions.length / 3 * 92 : 0) + count * 24 +
			(primitive.ribbon ?
				count *
				Math.max( 2, 3 * ((primitive.emission?.capacity ?? primitive.emission?.births.length ?? 1) - 1) + 1 ) *
				2 *
				160 :
				0) +
			palettes( primitive, instances ) + slots * 112 + instances * 96 +
			// The row-state table (ROW_STATE_FLOATS a slot) exists from two rows on.
			(count > 1 && characterRowGlow( primitive ) ? slots * ROW_STATE_FLOATS * 4 : 0) +
			(characterSharedMesh( primitive ) ?
				// A shared cloth piece keeps a skin table of its own: the skin
				// entries, a slot map of one entry a vertex, then its solver's
				// stream of one entry a slot (device/geometry.ts).
				primitive.cloth ?
					(geometry.joints?.length ?? 0) * 8 * 2 +
					characterClothSlots( primitive.cloth, caches ).length * CLOTH_STREAM_WORDS * 4 :
					0 :
				geometry.positions.length / 3 * 14 * 4 * 2 + geometry.indices.byteLength +
				(geometry.joints?.length ?? 0) * 8 * 2) +
			64 + 96 + 20;
	}, 0 );
}
/*
================
characterBytes

The resident bytes of one decoded source; throws when its structure or
decode exceeds the limits.
================
*/
export function characterBytes(
	model: CharacterModel,
	images: readonly (NativeTexture | { width: number; height: number; })[]
): number {
	if (
		model.nodes.length > CHARACTER_NODES || model.primitives.length > CHARACTER_PRIMITIVES ||
		model.images.length > CHARACTER_IMAGES || images.length !== model.images.length ||
		images.some( ( image, index ) =>
			image.width !== model.images[index]!.width || image.height !== model.images[index]!.height
		)
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
	const bytes = model.primitives.reduce(
		( sum, p ) => sum + clothBytes( clothData( p.cloth, p.geometry.positions.length / 3 ) ),
		0
	) + characterMaterialClockBytes( model ) + graphBytes +
		model.primitives.reduce( ( sum, p ) => sum + (p.emission?.births.length ?? 0) * 8, 0 ) +
		[ ...buffers ].reduce( ( sum, buffer ) => sum + buffer.byteLength, 0 );
	const pixels = images.reduce(
		( sum, image ) =>
			sum + ("kind" in image ? validateNativeTexture( image ) : imageBytes( image.width, image.height )),
		0
	);
	const total = bytes + pixels +
		images.reduce( ( sum, image ) => sum + ("kind" in image ? 0 : image.width * image.height), 0 );
	if ( bytes > CHARACTER_MODEL_BYTES || pixels > DECODED_IMAGE_BYTES || total > CHARACTER_SOURCE_BYTES ) {
		throw new Error( "Character decoded bytes exceed budget" );
	}
	return total;
}
