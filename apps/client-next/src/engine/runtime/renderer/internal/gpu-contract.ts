/*
===========================================================================

gpu-contract.ts - the renderer's internal device contracts

Draw handles and the command interfaces the device owners implement
(geometry, images, UI, frame) for the renderer modules.

===========================================================================
*/
import type { UiScene } from "@/engine/contracts/ui";
import type { Geometry } from "@/engine/contracts/geometry";
import type { Disposable, RuntimePhase, Viewport } from "@/engine/contracts/runtime";
// Narrow capabilities: device and queue never leave their owner.
export interface FrameCommands {
	prepare?( encoder: GPUCommandEncoder, timing?: GpuTimingFrame ): void;
	beginTiming?( frameId?: number ): GpuTimingFrame | undefined;
	createBundleEncoder( depth?: boolean ): GPURenderBundleEncoder;
	createEncoder(): GPUCommandEncoder;
	submit( buffer: GPUCommandBuffer ): void;
}
export interface DepthTarget extends Disposable {
	readonly view: GPUTextureView;
}
export interface ColorTarget extends DepthTarget {
	present( target: GPUTexture ): void;
}
export interface SurfaceCommands {
	createColor( width: number, height: number ): ColorTarget;
	createDepth( width: number, height: number ): DepthTarget;
	configure( context: GPUCanvasContext, format: GPUTextureFormat ): void;
}
export interface FlareInput {
	readonly uniforms: Float32Array;
	readonly textures: readonly ImageDraw[];
}
export interface FlareDraw {
	readonly compute: GPUComputePipeline;
	readonly binding: GPUBindGroup;
	readonly entries: readonly {
		readonly pipeline: GPURenderPipeline;
		readonly binding: GPUBindGroup;
		readonly index: number;
		readonly count: number;
	}[];
}
export interface BloomDraw {
	readonly view: GPUTextureView;
	encode( encoder: GPUCommandEncoder, target: GPUTextureView ): void;
}
export interface DeviceOwner extends Disposable {
	/** Bracket one frame, from its first preparation to its last submit: a GPU
	 * resource released in between outlives the command buffers that name it. */
	beginFrame(): void;
	endFrame(): void;
	particleQuery(
		points: Float32Array,
		matrix: Float32Array,
		color: GPUTextureView,
		depth: GPUTextureView
	): Promise<readonly boolean[]>;
	textureOptions( filtered: boolean, detail: number ): void;
	bloom( width: number, height: number, enabled: boolean ): BloomDraw | undefined;
	gpuTiming(): GpuTimingStats | null;
	portraitTarget( id?: string, width?: number, height?: number ): GPUTextureView;
	uiTexture( id: string, image: ImageBitmap | ImageData | null ): void;
	ui( scene: UiScene | null ): readonly UiDraw[];
	worldView( transform: Float32Array, environment: Float32Array ): void;
	sky(): ImageDraw | null;
	thunder( color: readonly number[] ): ImageDraw;
	flares( input: FlareInput, depth: GPUTextureView ): FlareDraw;
	geometry(): GeometryCommands | null;
	images(): ImageCommands | null;
	recoverable(): boolean;
	phase(): RuntimePhase;
	error(): string | null;
	commands(): FrameCommands | null;
	surfaceCommands(): SurfaceCommands | null;
	format(): GPUTextureFormat;
}
export interface SurfaceOwner extends Disposable {
	depth(): GPUTextureView;
	acquire( viewport: Viewport, offscreen?: boolean ): GPUTextureView;
	present(): void;
}
export interface DeferredDraw {
	readonly asynchronous: boolean;
	prepare(): readonly GeometryDraw[] | Promise<readonly GeometryDraw[]>;
}
export interface FrameOwner {
	draw(
		view: GPUTextureView,
		image?: ImageDraw,
		geometry?: GeometryDraw,
		depth?: GPUTextureView,
		world?: readonly GeometryDraw[],
		ui?: readonly UiDraw[],
		preview?: readonly GeometryDraw[],
		flares?: FlareDraw,
		thunder?: ImageDraw,
		portrait?: {
			readonly target: GPUTextureView;
			readonly depth: GPUTextureView;
			readonly draws: readonly GeometryDraw[];
		},
		doll?: {
			readonly target: GPUTextureView;
			readonly depth: GPUTextureView;
			readonly draws: readonly GeometryDraw[];
		},
		partyPortraits?: readonly {
			readonly target: GPUTextureView;
			readonly depth: GPUTextureView;
			readonly draws: readonly GeometryDraw[];
		}[],
		frameId?: number,
		deferred?: DeferredDraw,
		bloom?: BloomDraw
	): void | Promise<void>;
}

export interface ImageDraw {
	readonly pipeline: GPURenderPipeline;
	readonly binding: GPUBindGroup;
}
export interface ImageCommands {
	upload(
		image: import("@/engine/contracts/texture").WorldTexture,
		frames?: readonly import("@/engine/contracts/texture").WorldTexture[],
		mipmaps?: boolean
	): ImageDraw;
	release( draw: ImageDraw ): void;
}

export interface GeometryDraw {
	readonly deferredParticle?: boolean;
	readonly blended?: boolean;
	readonly pipeline: GPURenderPipeline;
	readonly binding: GPUBindGroup;
	readonly vertices: GPUBuffer;
	readonly indices: GPUBuffer;
	readonly count: number;
	readonly indexCount: number;
	readonly instanceCount: number;
	readonly instanceCapacity: number;
}
// Indexed palettes share the exact source storage supplied at upload. Offsets
// are matrix indices, one per instance; updateBones requires a monotonic revision
// and returns actual uploaded bytes (zero for an already published revision).
export interface CharacterShadowRequest {
	readonly matrix: Float32Array;
	readonly receiver: Geometry;
	readonly blob: boolean;
	readonly parts: readonly { readonly draw: GeometryDraw; readonly instance: number; }[];
}
/*
================
ParticlePresentation

One emitted primitive's particles for the GPU presentation pass
(device/particle-shader.ts): rows actors of slots particle slots each.
records and actors use the layouts of foundation/animation/
particle-records.ts. The owner rewrites records only at a tick and marks
the slots it rewrote in [dirtyStart, dirtyEnd); it writes actors and
axes every frame.
================
*/
export interface ParticlePresentation {
	readonly rows: number;
	readonly slots: number;
	// The particles belong to a particle graph (one tick fraction an actor).
	readonly graph: boolean;
	// ParticleView: none, camera, y or v (effect-billboard.ts).
	readonly view: number;
	readonly lifetime: number;
	readonly loop: boolean;
	readonly frames?: import("@/engine/contracts/character").CharacterPrimitive["materialFrames"];
	readonly records: Float32Array;
	readonly actors: Float32Array;
	// The camera basis of the view mode: three columns of four floats.
	readonly axes: Float32Array;
	dirtyStart: number;
	dirtyEnd: number;
}
/*
================
DrawRelease

When and from where a draw was released. A released draw's buffers are
retired at the end of the releasing frame, so any later frame that still
lists it would submit destroyed storage ("used in submit while destroyed").
================
*/
export interface DrawRelease {
	readonly atMs: number;
	readonly stack: string;
}
export interface GeometryCommands {
	// The release record of a draw no owner may draw any more; undefined
	// while the draw is live.
	releasedDraw?( draw: GeometryDraw ): DrawRelease | undefined;
	characterShadows?( requests: readonly CharacterShadowRequest[], blob?: ImageDraw ): readonly GeometryDraw[];
	// Null samples are CPU-owned slots already materialized in source; GPU samples preserve their canonical indices.
	prepareGpuBones?(
		source: Float32Array,
		model: import("@/engine/contracts/character").CharacterModel,
		primitive: import("@/engine/contracts/character").CharacterPrimitive,
		samples: readonly (
			| { readonly clip: import("@/engine/contracts/character").CharacterClip; readonly time: number; }
			| null
		)[],
		revision: number
	): boolean;
	gpuAnimationStats?(): {
		enabled: boolean;
		ready: boolean;
		models: number;
		streams: number;
		staticBytes: number;
		streamBytes: number;
		dispatches: number;
		poses: number;
	};
	updateMaterialColors( draw: GeometryDraw, rgb: Float32Array, flags: number ): void;
	updateTextureFactor( draw: GeometryDraw, rgba: Float32Array ): void;
	updateEquipmentGlow(
		draw: GeometryDraw,
		color: Float32Array,
		uv: Float32Array,
		gain: number,
		alphaTest: boolean,
		enabled: boolean
	): void;
	updateTextureTransform( draw: GeometryDraw, matrix: Float32Array ): void;
	updateBones( draw: GeometryDraw, bones: Float32Array, revision?: number ): number;
	// The draw's instances and palettes come from the particle pass this
	// frame; the draw has one instance and one joint a slot.
	presentParticles( draw: GeometryDraw, particles: ParticlePresentation ): void;
	updateIndices( draw: GeometryDraw, indices: Uint32Array ): void;
	// ranges are vertex start/count pairs within positions. Without a slot the
	// positions cover the whole draw; with one (0 included) they are a terrain
	// layer member's vertices at that vertex offset of the draw.
	updatePositions(
		draw: GeometryDraw,
		positions: Float32Array,
		colors?: Float32Array,
		uvs?: Float32Array,
		ranges?: readonly (readonly [number, number])[],
		slot?: number
	): void;
	// Writes a packed vertex stream (14 floats a vertex) at vertex base of a
	// dynamicVertices draw: a terrain layer member taking its slot.
	writeVertices( draw: GeometryDraw, base: number, vertices: Float32Array ): void;
	updateInstances(
		draw: GeometryDraw,
		instances: Float32Array,
		opacity?: Float32Array,
		appearance?: Float32Array,
		pointLights?: Float32Array,
		paletteOffsets?: Uint32Array
	): GeometryDraw;
	updateTransform( draw: GeometryDraw, transform: Float32Array ): void;
	upload(
		data: Geometry,
		texture?: ImageDraw,
		paletteOffsets?: Uint32Array,
		environmentImage?: ImageDraw
	): GeometryDraw;
	release( draw: GeometryDraw ): void;
}
export interface PreparedWorld {
	readonly camera: import("@/engine/contracts/scene").WorldCamera;
	readonly groundDecalDraws?: readonly GeometryDraw[];
	readonly terrainEnd?: number;
	readonly decalDraws?: readonly GeometryDraw[];
	readonly weatherDraws?: readonly GeometryDraw[];
	/** Character insertion boundary; preserve world pass order on both sides. */
	readonly thunder?: readonly number[] | null;
	readonly flares?: FlareInput;
	readonly transparentStart: number;
	readonly originRegion: number;
	readonly matrix: Float32Array;
	readonly draws: readonly GeometryDraw[];
	readonly environment: Float32Array;
	readonly sky: boolean;
}

export interface UiDraw {
	readonly layer?: "background" | "world";
	readonly pipeline: GPURenderPipeline;
	readonly binding: GPUBindGroup;
	readonly first: number;
	count: number;
}

export type GpuTimingStats = import("@/engine/contracts/runtime").GpuTimingStats;
export interface GpuTimingFrame {
	pass( name: string ): GPURenderPassTimestampWrites | undefined;
	resolve(): { query: GPUQuerySet; count: number; resolve: GPUBuffer; read: GPUBuffer; } | undefined;
	submitted(): void;
}
