/*
===========================================================================

geometry.ts - GPU geometry resources

Uploads meshes to GPU buffers and bind groups and owns their updates
(instances, bones, indices, positions) and release. Only meshes uploaded
with dynamicVertices keep a CPU mirror for position updates. An immutable
skinned mesh uploaded by several draws (a body part or worn item in every
character batch that holds it) has one GPU copy of its vertex stream,
indices and skin table, shared by those draws (sharedMesh). A terrain
layer keeps none: its members own their packed streams, and streamVertices
writes a member's stream (or a patched range of it) straight to its slot.

A material uploaded with dynamicMaterial (a character primitive with a
material clock) lives in a staged slab set: its per-frame updates land in
a CPU mirror, and flushMaterials sends them in one write a slab before
each frame submit. Every other material is written once and its rare
updates go straight to the queue.

A draw uploaded with rowStates also owns a row-state table beside its
instances (binding 16, ROW_STATE_FLOATS a row): each row's equipment glow
colour and UV, which its pipelines' shader variant (pipelines.ts
geometrySource) reads in place of the material's. Rows of several actors
then share one draw and keep their own glow phases. The table lives in a
staged slab set too, so a frame's row states reach the GPU in the same
flush. Every other draw keeps the plain shader and a bind group without
binding 16.

===========================================================================
*/
import {
	createInstancePacking,
	instanceCapacity,
	ROW_STATE_FLOATS
} from "@/engine/foundation/rendering/geometry-instances";
import { CLOTH_NO_SLOT, CLOTH_STREAM_WORDS, validClothStreamSlots } from "@/engine/foundation/animation/cloth-stream";
import { createWaterReflection } from "./water-reflection";
import { packTextureStage } from "@/engine/foundation/rendering/texture-stage";
import { dungeonHeightFog } from "@/engine/foundation/rendering/dungeon-fog";
import { createCharacterShadows } from "./character-shadows";
import { createSunShadow } from "./sun-shadow";
import { ENVIRONMENT_SHADOW_MATRIX_OFFSET } from "./environment-block";
import { destroyNow, type Retire } from "./retirement";
import { MSAA_SAMPLE_COUNT } from "./msaa";
import { createBufferSlabs, createStagedSlabs, type BufferSlice } from "./buffer-slabs";
import { DeviceDraw } from "./device-draw";
import { createGeometryUploads } from "./geometry-uploads";
import type { createParticlePresentation } from "./particles";
import type { createGpuAnimationResources } from "./animation";
import { DEFAULT_BLEND, type GeometryPipelineState, type SceneSurface } from "./pipelines";
import { D3DBLEND_SRCCOLOR, D3DBLEND_ZERO, type BlendPair } from "@/engine/foundation/rendering/blend-state";
import type { Geometry } from "@/engine/contracts/geometry";
import { meshArrays, packGeometryVertices, sameMesh } from "@/engine/foundation/rendering/geometry-vertices";
import type { GeometryCommands, GeometryDraw, ImageDraw } from "@/engine/runtime/renderer/internal/gpu-contract";
import type { ImageLeases } from "./images";

// A bound slab range: buffer, offset and size are all set.
type Binding = BufferSlice["binding"];
/*
================
SharedMesh

One GPU copy of an immutable skinned mesh. arrays are the CPU arrays the
copy is made from (geometry-vertices.ts meshArrays); refs counts the draws
using it.
================
*/
interface SharedMesh {
	readonly arrays: readonly unknown[];
	readonly vertices: GPUBuffer;
	readonly indices: GPUBuffer;
	readonly skin: GPUBuffer;
	refs: number;
}
// The packed vertex (14 floats) and a uint32 index, in bytes.
const VERTEX_BYTES = 56;
const INDEX_BYTES = 4;
// policy.y, the local fog a material carries; the shader tests above 1.5.
const LOCAL_FOG_LINEAR = 1;
const LOCAL_FOG_SHADER = 2;
/*
================
geometryPipelineState

The pipeline state a material draws with. Blending follows the material's
D3D pair (SRCALPHA/INVSRCALPHA when it names none); a lightmap multiplies
(ZERO/SRCCOLOR). Depth writes follow the material, else a blended draw
writes depth only as a fading object or a decal, as the old pipeline
classes did. Sky, deferred particles and ground decals draw without the
depth test (the second sky, culled, keeps it).
================
*/
export function geometryPipelineState( mat: Geometry["material"] ): GeometryPipelineState {
	const cull = !!mat && !mat.doubleSided;
	if ( mat?.groundDecal ) return { blend: DEFAULT_BLEND, cull, depthWrite: false, depthCompare: "always" };
	const fade = !!(mat?.objectFade || mat?.instanceFade);
	const blend: BlendPair | null = mat?.lightmap ?
		{ source: D3DBLEND_ZERO, destination: D3DBLEND_SRCCOLOR } :
		mat?.blend || fade || mat?.decal || mat?.sky ?
		mat.blendPair ?? DEFAULT_BLEND :
		null;
	const writes = mat?.depthWrite ?? ((!blend || fade || !!mat?.decal) && !mat?.sky && !mat?.lightmap);
	if ( mat?.deferredParticle ) return { blend, cull, depthWrite: false, depthCompare: "always" };
	const untested = !!mat?.sky && !(mat.sky === 2 && cull);
	return { blend, cull, depthWrite: writes, depthCompare: untested ? "always" : "less-equal" };
}

/*
================
createGeometryResources
================
*/
/*
================
markCloth

A cloth draw's instance records carry its vertex count V in opacity.w
(record float 19), which the vertex shaders read; every other draw keeps 0
there. Its skin table holds the skin entries [0, V), the slot map [V, 2V)
and the stream from 2V on (upload).
================
*/
function markCloth( packed: Float32Array, clothBase: number ): Float32Array {
	if ( clothBase ) {
		for ( let at = 19; at < packed.length; at += 40 ) packed[at] = clothBase;
	}
	return packed;
}

export function createGeometryResources(
	created: GPUDevice,
	current: () => GPUDevice,
	fail: ( error: unknown ) => void,
	// The geometry pipeline for state on a scene surface: the HDR stage's
	// rgba16float intermediate, the MSAA stage's 4x sibling, or the
	// preview pass's coverage sibling.
	// rowStates selects the variant a draw with row states needs (Geometry.rowStates).
	pipelines: ( state: GeometryPipelineState, surface?: SceneSurface, rowStates?: boolean ) => GPURenderPipeline,
	// Each draw holds a lease on its images for as long as it binds them.
	images: ImageLeases,
	worldSampler: GPUSampler,
	lightmapSampler: GPUSampler,
	environment: GPUBuffer,
	sampling?: ( filtered: boolean, detail: number, anisotropic: boolean ) => GPUSampler,
	animation?: ReturnType<typeof createGpuAnimationResources>,
	format: GPUTextureFormat = "rgba8unorm",
	particles?: ReturnType<typeof createParticlePresentation>,
	// A frame may still name a released buffer: the device decides when it dies.
	retire: Retire = destroyNow,
	// The clamped sampler for the anisotropy stage; without it the lightmap
	// sampler stays the one given.
	lightmapSampling?: ( anisotropic: boolean ) => GPUSampler
) {
	let filtered = true, detail = 2, anisotropic = false, mixedCpuUploadBytes = 0;
	// The HDR stage's scene intermediate flips the colour format every
	// geometry pipeline renders into, and the MSAA stage flips the sample
	// count; sceneBindings swaps every draw over to the current pair.
	let floatScene = false, msaaScene = false, shadowEnabled = false;
	const waterReflection = createWaterReflection( created, format, retire );
	// 8BA130 fixes water MIN/MAG to linear independently of the video filter.
	const waterSampler = worldSampler;
	const reflectedBindings = new WeakMap<GeometryDraw, { source: GPUBindGroup; draw: GeometryDraw; }>();
	const previewBindings = new WeakMap<GeometryDraw, { source: GPUBindGroup; draw: GeometryDraw; }>();
	const singleSampledBindings: typeof previewBindings = new WeakMap();
	const geometryBuffers = new Map<GeometryDraw, GPUBuffer[]>();
	// The shared skinned meshes by their positions array, and the one each
	// draw uses (sharedMesh, releaseMesh).
	const sharedMeshes = new Map<Float32Array, SharedMesh[]>();
	const drawMeshes = new Map<GeometryDraw, SharedMesh>();
	/*
	================
	sharedMesh

	The GPU copy of an immutable skinned mesh, made on its first upload and
	counted on every later one. The skin table interleaves each vertex's four
	joints and four weights, as the skinning shaders read it; a cloth draw
	reads its own table instead, which holds its solver's stream as well.
	================
	*/
	function sharedMesh( gpu: GPUDevice, data: Geometry ): SharedMesh {
		const arrays = meshArrays( data ), list = sharedMeshes.get( data.positions ) ?? [];
		const found = list.find( mesh => sameMesh( mesh.arrays, arrays ) );
		if ( found ) {
			found.refs++;
			return found;
		}
		const interleaved = data.vertices?.length === data.positions.length / 3 * 14 ?
			data.vertices :
			packGeometryVertices( data );
		const packed = new ArrayBuffer( data.joints!.length * 8 ),
			joints = new Uint32Array( packed ),
			weights = new Float32Array( packed );
		for ( let v = 0; v < data.joints!.length / 4; v++ ) {
			for ( let c = 0; c < 4; c++ ) {
				joints[v * 8 + c] = data.joints![v * 4 + c]!;
				weights[v * 8 + 4 + c] = data.weights![v * 4 + c]!;
			}
		}
		const made: GPUBuffer[] = [];
		const store = ( label: string, source: Float32Array | Uint32Array, usage: number ) => {
			const buffer = gpu.createBuffer( {
				label,
				size: source.byteLength,
				usage: usage | GPUBufferUsage.COPY_DST
			} );
			made.push( buffer );
			gpu.queue.writeBuffer( buffer, 0, source.buffer as ArrayBuffer, source.byteOffset, source.byteLength );
			return buffer;
		};
		try {
			const mesh: SharedMesh = {
				arrays,
				vertices: store( "geometry-vertices", interleaved, GPUBufferUsage.VERTEX ),
				indices: store( "geometry-indices", data.indices, GPUBufferUsage.INDEX ),
				skin: store( "geometry-skin", joints, GPUBufferUsage.STORAGE ),
				refs: 1
			};
			list.push( mesh );
			sharedMeshes.set( data.positions, list );
			return mesh;
		} catch ( error ) {
			for ( const buffer of made ) buffer.destroy();
			throw error;
		}
	}
	/*
	================
	releaseMesh

	Drops one draw's use of a shared mesh; the last one retires its buffers.
	================
	*/
	function releaseMesh( mesh: SharedMesh | undefined ) {
		if ( !mesh || --mesh.refs > 0 ) return;
		const positions = mesh.arrays[0] as Float32Array, list = sharedMeshes.get( positions ) ?? [];
		list.splice( list.indexOf( mesh ), 1 );
		if ( !list.length ) sharedMeshes.delete( positions );
		retire( mesh.vertices );
		retire( mesh.indices );
		retire( mesh.skin );
	}
	// Released draws and their release records (releasedDraw).
	const releasedDraws = new WeakMap<GeometryDraw, { atMs: number; error: Error; }>();
	// Every buffer is labelled: a validation error names the resource it rejects.
	const worldUniform = created.createBuffer( {
		label: "geometry-world",
		size: 64,
		usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST
	} );
	const white = created.createTexture( {
		size: [ 1, 1 ],
		format: "rgba8unorm",
		usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST
	} );
	created.queue.writeTexture( { texture: white }, Uint8Array.of( 255, 255, 255, 255 ), { bytesPerRow: 4 }, [ 1, 1 ] );
	// Bindings 13/14, the sun cascade every world draw samples: while the
	// stage is off the draws bind a 1x1 depth view and the gated shader
	// branch never samples it, so its contents are irrelevant and the
	// native frame is untouched; while it is on, the draws bind the real
	// cascade view. No encoder is spent clearing a view nothing reads.
	const shadowCompare = created.createSampler( {
		compare: "less",
		minFilter: "linear",
		magFilter: "linear",
		addressModeU: "clamp-to-edge",
		addressModeV: "clamp-to-edge"
	} );
	const shadowDummy = created.createTexture( {
		label: "sun-shadow-dummy",
		size: [ 1, 1 ],
		format: "depth24plus",
		usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING
	} );
	const shadowDummyView = shadowDummy.createView();
	// Binding 15, the ambient occlusion factor (experimental, #2094):
	// while the stage is off the draws bind the 1x1 white view (factor
	// 1, no occlusion) and the gated shader read never samples it;
	// while it is on, the blurred factor's view (device.ts installs it
	// through refreshAmbientOcclusion when the view changes).
	const aoDummyView = white.createView();
	let aoBoundView = aoDummyView;
	const uploads = createGeometryUploads( created, retire );
	const packInstances = createInstancePacking();
	// Per-draw uniforms and instance tables are slab ranges (buffer-slabs.ts):
	// thousands of small buffers each cost the GPU process an allocation.
	const instanceUsage = GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC,
		uniformUsage = GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST;
	const transformSlabs = createBufferSlabs( created, "geometry-transform", uniformUsage, retire ),
		materialSlabs = createBufferSlabs( created, "geometry-material", uniformUsage, retire ),
		instanceSlabs = createBufferSlabs( created, "geometry-instances", instanceUsage, retire );
	// Materials rewritten every frame, apart from the static ones: a flush
	// writes a slab's span from its lowest to its highest written range.
	const dynamicMaterialSlabs = createStagedSlabs( created, "geometry-material-dynamic", uniformUsage, retire );
	// updateEquipmentGlow's packed vec4 pair, reused by every call.
	const glowUniform = new Float32Array( 8 );
	// Row states (Geometry.rowStates), rewritten every frame like the
	// dynamic materials and flushed with them.
	const rowStateSlabs = createStagedSlabs(
		created,
		"geometry-row-states",
		GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
		retire
	);
	// Vertex and index ranges align to their element: a draw addresses them
	// by baseVertex and firstIndex (VERTEX_BYTES is the packed 14-float vertex).
	const vertexSlabs = createBufferSlabs(
			created,
			"geometry-vertices",
			GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
			retire,
			VERTEX_BYTES
		),
		indexSlabs = createBufferSlabs(
			created,
			"geometry-indices",
			GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST,
			retire,
			INDEX_BYTES
		);
	// World draws bind the shared world uniform, not a transform of their own.
	const worldBinding: Binding = Object.freeze( { buffer: worldUniform, offset: 0, size: 64 } );
	// A particle presentation copies whole instance tables (particles.ts), so
	// a draw that may present one keeps a buffer of its own.
	const ownedInstances = new Set<GPUBuffer>();
	/*
	================
	instanceStore
	================
	*/
	function instanceStore( bytes: number, owned: boolean ): BufferSlice {
		if ( !owned ) return instanceSlabs.allocate( bytes );
		const buffer = created.createBuffer( { label: "geometry-instances", size: bytes, usage: instanceUsage } );
		ownedInstances.add( buffer );
		return {
			binding: Object.freeze( { buffer, offset: 0, size: bytes } ),
			destroy() {
				ownedInstances.delete( buffer );
				buffer.destroy();
			}
		};
	}
	/*
	================
	rowStateStore

	A rowStates draw's table for capacity rows, zeroed: a staged mirror is
	its range's whole truth, and a recycled range holds another draw's rows.
	================
	*/
	function rowStateStore( capacity: number ): BufferSlice {
		const floats = capacity * ROW_STATE_FLOATS, store = rowStateSlabs.allocate( floats * 4 );
		rowStateSlabs.write( store, new Float32Array( floats ), floats * 4 );
		return store;
	}
	const defaultSkin = created.createBuffer( {
			label: "geometry-default-skin",
			size: 32,
			usage: GPUBufferUsage.STORAGE
		} ),
		defaultBones = created.createBuffer( {
			label: "geometry-default-bones",
			size: 64,
			usage: GPUBufferUsage.STORAGE
		} );
	/*
	================
	SharedPalette
	================
	*/
	type SharedPalette = { source: Float32Array; buffer: GPUBuffer; refs: number; revision: number; };
	const sharedPalettes = new Map<Float32Array, SharedPalette>();
	/*
	================
	releasePalette
	================
	*/
	function releasePalette( palette: SharedPalette | undefined ) {
		if ( palette && !--palette.refs ) {
			animation?.release( palette.source );
			retire( palette.buffer );
			sharedPalettes.delete( palette.source );
		}
	}
	/*
	================
	validatePaletteOffsets
	================
	*/
	function validatePaletteOffsets( offsets: Uint32Array | undefined, boneBytes: number, jointMaximum: number ) {
		if ( !offsets ) throw Error( "Palette offset outside bone storage" );
		const joints = boneBytes / 64;
		for ( let i = 0; i < offsets.length; i++ ) {
			if ( offsets[i]! + jointMaximum >= joints ) throw Error( "Palette offset outside bone storage" );
		}
	}
	/*
	================
	writeMaterial

	data at byte at of the draw's material uniform: into the staged mirror
	for a dynamic material, straight to the queue for a static one (a caller
	that updates a material it did not upload as dynamic still gets its
	bytes, as before).
	================
	*/
	function writeMaterial(
		meta: { dynamicMaterial: boolean; material: Binding; stores: { material: BufferSlice; }; },
		at: number,
		data: Float32Array
	) {
		if ( meta.dynamicMaterial ) {
			dynamicMaterialSlabs.write( meta.stores.material, data, data.byteLength, at );
			return;
		}
		current().queue.writeBuffer(
			meta.material.buffer,
			meta.material.offset + at,
			data as Float32Array<ArrayBuffer>
		);
	}
	/*
	================
	writeStream

	GPU-written palettes and particle streams retain their immediate path.
	================
	*/
	function writeStream( buffer: GPUBuffer, offset: number, data: Float32Array, cpu = true ) {
		const gpu = current();
		if ( cpu ) uploads.write( buffer, offset, data );
		else gpu.queue.writeBuffer( buffer, offset, data.buffer as ArrayBuffer, data.byteOffset, data.byteLength );
	}
	/*
	================
	geometryBinding
	================
	*/
	const geometryBinding = (
		uniform: Binding,
		storage: Binding,
		material: Binding,
		// Binding 16, only for a draw with row states (its pipeline's variant).
		rowStates: Binding | undefined,
		pipeline: GPURenderPipeline,
		clampedSampling: boolean,
		image?: ImageDraw,
		skin = defaultSkin,
		bones = defaultBones,
		environmentImage?: ImageDraw,
		capture = false
	) => {
		const cascade = shadowEnabled ? ensureSunShadow().cascadeView() : shadowDummyView;
		return current().createBindGroup( {
			layout: pipeline.getBindGroupLayout( 0 ),
			entries: [
				{ binding: 9, resource: capture ? white.createView() : waterReflection.view() ?? white.createView() },
				{ binding: 10, resource: { buffer: capture ? waterReflection.capture : waterReflection.main } },
				{ binding: 11, resource: lightmapSampler },
				{ binding: 12, resource: waterSampler },
				{ binding: 13, resource: cascade },
				{ binding: 14, resource: shadowCompare },
				{ binding: 15, resource: aoBoundView },
				{ binding: 0, resource: uniform },
				{ binding: 1, resource: storage },
				{ binding: 2, resource: material },
				...(rowStates ? [ { binding: 16, resource: rowStates } ] : []),
				{
					binding: 3,
					resource: clampedSampling ? lightmapSampler : worldSampler
				},
				{
					binding: 4,
					resource: (image ? images.texture( image ) : white).createView( { dimension: "2d-array" } )
				},
				{ binding: 5, resource: { buffer: environment } },
				{ binding: 6, resource: { buffer: skin } },
				{ binding: 7, resource: { buffer: bones } },
				{
					binding: 8,
					resource: (environmentImage ? images.texture( environmentImage ) : white).createView( {
						dimension: "2d-array"
					} )
				}
			]
		} );
	};
	const metadata = new Map<GeometryDraw, {
		water: boolean;
		state: GeometryPipelineState;
		uniform: Binding;
		material: Binding;
		instances: Binding;
		// Binding 16: the row-state table of a rowStates draw.
		rowStates?: Binding;
		// The slab ranges behind the three bindings (no transform for a world
		// draw); owned marks an instance table with a buffer of its own.
		stores: {
			transform?: BufferSlice;
			material: BufferSlice;
			instances: BufferSlice;
			// The row-state table of a rowStates draw (rowStateSlabs).
			rowStates?: BufferSlice;
			owned: boolean;
			// Absent for a skinned draw, whose streams are buffers of its own.
			vertices?: BufferSlice;
			indices?: BufferSlice;
		};
		image?: ImageDraw;
		environmentImage?: ImageDraw;
		vertices?: Float32Array;
		// The vertex buffer's length in floats (14 a vertex), the bound of a
		// streamVertices write.
		vertexFloats: number;
		skin: GPUBuffer;
		bones: GPUBuffer;
		// A cloth mesh's vertex count, where its slot map starts in the skin
		// table, its stream starting at twice that (cloth-stream.ts); 0 for
		// any other draw.
		clothBase: number;
		// The cloth stream's entries: one a slot.
		clothSlots: number;
		// The bone and index buffers' sizes, read once: a GPUBuffer's size
		// crosses into the browser on every read, and updates run every frame.
		boneBytes: number;
		// Particle-capable owned palettes and instances may be written by compute.
		cpuUploads: boolean;
		indexBytes: number;
		palette?: SharedPalette;
		jointMaximum: number;
		// Lightmaps and decals sample with clamped addressing.
		clampedSampling: boolean;
		// The material uniform's TEXTUREFACTOR offset in bytes (its last vec4).
		textureFactorOffset: number;
		// stores.material is a dynamicMaterialSlabs range (Geometry.dynamicMaterial).
		dynamicMaterial: boolean;
		// The draw handle itself, which this owner updates in place.
		selection: DeviceDraw;
	}>();
	let shadows: ReturnType<typeof createCharacterShadows> | undefined;
	// The experimental sun cascade: created on first use (the renderer
	// prepares it once the stage is on), borrowing each caster's buffers
	// through the metadata resolver, as the silhouette shadows do.
	let sunShadows: ReturnType<typeof createSunShadow> | undefined;
	const ensureSunShadow = () =>
		sunShadows ??= createSunShadow(
			current(),
			environment,
			ENVIRONMENT_SHADOW_MATRIX_OFFSET,
			draw => {
				const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
				return meta && buffers ?
					{
						instances: meta.instances,
						material: meta.material,
						skin: meta.skin,
						bones: meta.bones,
						image: meta.image ? images.texture( meta.image ) : undefined
					} :
					undefined;
			},
			retire
		);
	/*
	================
	rebindEveryDraw

	A shared binding (the sun cascade, the occlusion factor) now names another view. Bind groups hold their
	views, so every draw rebinds against its current pipeline through
	the one geometryBinding funnel.
	================
	*/
	function rebindEveryDraw() {
		for ( const [draw, meta] of metadata ) {
			DeviceDraw.rebind(
				meta.selection,
				geometryBinding(
					meta.uniform,
					meta.instances,
					meta.material,
					meta.rowStates,
					draw.pipeline,
					meta.clampedSampling,
					meta.image,
					meta.skin,
					meta.bones,
					meta.environmentImage
				)
			);
		}
	}
	/*
	================
	passSibling

	One draw's single-sampled sibling. Borrows buffers without changing source
	handles; bindings follow their source revision and counts are refreshed
	for each prepared frame.
	================
	*/
	function passSibling( draw: GeometryDraw, bindings: typeof previewBindings, previewCoverage: boolean ) {
		const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
		if ( !meta || !buffers ) throw Error( "Unknown or released sibling draw" );
		let cached = bindings.get( draw );
		if ( !cached || cached.source !== draw.binding ) {
			const pipeline = pipelines(
				meta.state,
				{ sceneFloat: floatScene, fullscreenPreview: previewCoverage },
				!!meta.rowStates
			);
			cached = {
				source: draw.binding,
				draw: {
					...draw,
					pipeline,
					binding: geometryBinding(
						meta.uniform,
						meta.instances,
						meta.material,
						meta.rowStates,
						pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage
					)
				}
			};
			bindings.set( draw, cached );
		}
		return {
			...cached.draw,
			indexCount: draw.indexCount,
			instanceCount: draw.instanceCount,
			instanceCapacity: draw.instanceCapacity
		};
	}
	const commands: GeometryCommands = Object.freeze( {
		/*
		================
		hdrPreview

		Port-only, not native: only the fullscreen preview resolve consumes
		coverage alpha. Portrait and doll UI textures retain authored alpha.
		The preview pass is single-sampled, so the MSAA stage needs siblings
		here even without HDR.
		================
		*/
		hdrPreview( draws: readonly GeometryDraw[] ): readonly GeometryDraw[] {
			if ( !floatScene && !msaaScene ) return draws;
			return draws.map( draw => passSibling( draw, previewBindings, floatScene ) );
		},
		/*
		================
		singleSampled

		Port-only, not native: the siblings of the MSAA stage's 4x draws for
		passes that stay single-sampled (portraits, dolls, deferred particles).
		================
		*/
		singleSampled( draws: readonly GeometryDraw[] ): readonly GeometryDraw[] {
			if ( !msaaScene ) return draws;
			return draws.map( draw => passSibling( draw, singleSampledBindings, false ) );
		},
		/*
        ================
        waterReflection

        Capture variants share geometry and palettes, but use an independent
        bind group. The native orbit camera preserves handedness; 8BA130 selects
        D3DCULL_CCW (clockwise fronts). Water never appears in its own image.
        ================
        */
		waterReflection(
			input: { matrix?: Float32Array; height: number; above: boolean; seconds: number; },
			draws: readonly GeometryDraw[]
		) {
			const { matrix, height, above, seconds } = input;
			if ( waterReflection.update( matrix, height, above, seconds ) ) {
				for ( const [draw, meta] of metadata ) {
					DeviceDraw.rebind(
						meta.selection,
						geometryBinding(
							meta.uniform,
							meta.instances,
							meta.material,
							meta.rowStates,
							draw.pipeline,
							meta.clampedSampling,
							meta.image,
							meta.skin,
							meta.bones,
							meta.environmentImage
						)
					);
				}
			}
			if ( !matrix ) return undefined;
			const reflected: GeometryDraw[] = [];
			for ( const draw of draws ) {
				const meta = metadata.get( draw );
				if ( !meta || meta.water || draw.deferredParticle ) continue;
				let cached = reflectedBindings.get( draw );
				if ( !cached || cached.source !== draw.binding ) {
					const pipeline = pipelines(
						meta.state,
						{ sceneFloat: floatScene, msaa: msaaScene },
						!!meta.rowStates
					);
					const binding = geometryBinding(
						meta.uniform,
						meta.instances,
						meta.material,
						meta.rowStates,
						pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage,
						true
					);
					cached = {
						source: draw.binding,
						draw: {
							...draw,
							pipeline,
							binding,
							vertices: draw.vertices,
							indices: draw.indices,
							count: draw.count,
							indexCount: draw.indexCount,
							instanceCount: draw.instanceCount,
							instanceCapacity: draw.instanceCapacity
						}
					};
					reflectedBindings.set( draw, cached );
				}
				reflected.push( { ...cached.draw, indexCount: draw.indexCount, instanceCount: draw.instanceCount } );
			}
			return {
				encode( encoder: GPUCommandEncoder ) {
					waterReflection.encode( encoder, reflected );
				}
			};
		},
		/*
		================
		characterShadows
		================
		*/
		characterShadows(
			requests: readonly import("../internal/gpu-contract").CharacterShadowRequest[],
			blob?: ImageDraw
		) {
			if ( !shadows && !requests.length ) return [];
			shadows ??= createCharacterShadows(
				current(),
				worldUniform,
				images.texture,
				draw => {
					const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
					return meta && buffers ?
						{ instances: meta.instances, material: meta.material, skin: meta.skin, bones: meta.bones } :
						undefined;
				},
				// The receiver draws land in the scene target: the getters
				// follow the HDR stage's format and the MSAA stage's samples.
				() => (floatScene ? "rgba16float" : format),
				() => (msaaScene ? MSAA_SAMPLE_COUNT : 1),
				retire
			);
			return shadows.prepare( requests, blob );
		},
		...(animation ?
			{
				gpuAnimationStats: () => ({ ...animation.stats(), cpuUploadBytes: mixedCpuUploadBytes }),
				prepareGpuBones: (
					source: Float32Array,
					model: import("@/engine/contracts/character").CharacterModel,
					primitive: import("@/engine/contracts/character").CharacterPrimitive,
					samples: readonly ({
						readonly clip: import("@/engine/contracts/character").CharacterClip;
						readonly time: number;
					} | null)[],
					revision: number
				) => {
					current();
					const palette = sharedPalettes.get( source );
					if ( !palette ) return false;
					if (
						!Number.isSafeInteger( revision ) || revision < 0 || revision < palette.revision
					) throw Error( "Invalid GPU palette revision" );
					if ( palette.revision === revision ) return true;
					if ( !animation.prepare( source, palette.buffer, model, primitive, samples ) ) return false;
					// CPU writes and GPU jobs own disjoint canonical pose slots.
					const stride = primitive.joints.length * 64;
					for ( let i = 0; i < samples.length; ) {
						if ( samples[i] !== null ) {
							i++;
							continue;
						}
						const first = i;
						while ( i < samples.length && samples[i] === null ) i++;
						const bytes = (i - first) * stride;
						current().queue.writeBuffer(
							palette.buffer,
							first * stride,
							source.buffer as ArrayBuffer,
							source.byteOffset + first * stride,
							bytes
						);
						mixedCpuUploadBytes += bytes;
					}
					palette.revision = revision;
					return true;
				}
			} :
			{}),
		/*
		================
		presentParticles
		================
		*/
		presentParticles( draw: GeometryDraw, presentation: import("../internal/gpu-contract").ParticlePresentation ) {
			const meta = metadata.get( draw ), buffers = geometryBuffers.get( draw );
			if ( !particles ) throw Error( "Particle presentation unavailable" );
			if ( !meta || !buffers || meta.bones === defaultBones || meta.palette ) {
				throw Error( "Particle presentation requires an owned skinned draw" );
			}
			if ( meta.selection.instanceCount !== presentation.rows * presentation.slots ) {
				throw Error( "Particle presentation must draw one instance a slot" );
			}
			if ( !meta.stores.owned ) throw Error( "Particle presentation requires owned instances" );
			particles.present( draw, meta.instances.buffer, meta.bones, presentation );
		},
		/*
		================
		updateBones
		================
		*/
		updateBones( draw: GeometryDraw, bones: Float32Array, revision?: number ) {
			const meta = metadata.get( draw );
			if ( !meta || meta.bones === defaultBones || bones.byteLength > meta.boneBytes ) {
				throw new Error( "Invalid bone palette" );
			}
			if ( meta.palette ) {
				if (
					typeof revision !== "number" || !Number.isSafeInteger( revision ) || revision < 0 ||
					revision < meta.palette.revision || bones.buffer !== meta.palette.source.buffer ||
					bones.byteOffset !== meta.palette.source.byteOffset
				) throw Error( "Invalid shared palette revision" );
				if ( meta.palette.revision === revision ) return 0;
			}
			if ( meta.palette ) animation?.cancel( meta.palette.source );
			writeStream( meta.bones, 0, bones, meta.cpuUploads && (!animation || !meta.palette) );
			if ( meta.palette ) meta.palette.revision = revision!;
			return bones.byteLength;
		},
		/*
		================
		updateIndices
		================
		*/
		updateIndices( draw: GeometryDraw, indices: Uint32Array ) {
			const gpu = current(), buffers = geometryBuffers.get( draw ), meta = metadata.get( draw );
			// Other draws read a shared mesh's indices.
			if ( drawMeshes.has( draw ) ) throw new Error( "A shared mesh's indices do not change" );
			if ( !buffers || !meta || indices.byteLength > meta.indexBytes ) {
				throw new Error( "Invalid index selection" );
			}
			if ( indices.byteLength ) {
				gpu.queue.writeBuffer(
					draw.indices,
					(draw.firstIndex ?? 0) * INDEX_BYTES,
					indices.buffer as ArrayBuffer,
					indices.byteOffset,
					indices.byteLength
				);
			}
			DeviceDraw.select( meta.selection, indices.length, 1 );
		},
		/*
		================
		updatePositions
		================
		*/
		updatePositions(
			draw: GeometryDraw,
			positions: Float32Array,
			colors?: Float32Array,
			uvs?: Float32Array,
			ranges?: readonly (readonly [number, number])[],
			slot?: number
		) {
			const meta = metadata.get( draw ), count = positions.length / 3;
			current();
			if ( !meta ) throw Error( "Invalid position update" );
			const mirror = meta.vertices;
			if ( !mirror ) throw Error( "Geometry was uploaded without dynamicVertices" );
			// A whole mesh (no slot) covers its draw exactly; a layer member at
			// any slot, the first one included, fits inside it.
			const base = slot ?? 0;
			if (
				!Number.isInteger( base ) || base < 0 ||
				(slot === undefined ? count !== mirror.length / 14 : base + count > mirror.length / 14) ||
				colors && colors.length !== count * 4 || uvs && uvs.length !== count * 2
			) throw Error( "Invalid position update" );
			const vertices = mirror.subarray( base * 14, (base + count) * 14 );
			// Ranges are vertex start/count pairs. Validate the complete transaction
			// before changing the retained CPU mirror or submitting any GPU writes.
			const spans: [number, number][] = [];
			for ( const [start, length] of ranges ?? [ [ 0, count ] ] ) {
				if (
					!Number.isInteger( start ) || !Number.isInteger( length ) || start < 0 || length < 0 ||
					start + length > count
				) throw Error( "Invalid position range" );
				if ( length ) spans.push( [ start, start + length ] );
			}
			spans.sort( ( a, b ) => a[0] - b[0] );
			let merged = 0;
			for ( const span of spans ) {
				const previous = spans[merged - 1];
				if ( previous && span[0] <= previous[1] ) previous[1] = Math.max( previous[1], span[1] );
				else spans[merged++] = span;
			}
			spans.length = merged;
			for ( const [start, end] of spans ) {
				for ( let i = start; i < end; i++ ) {
					for ( let c = 0; c < 3; c++ ) {
						if ( !Number.isFinite( positions[i * 3 + c] ) ) throw Error( "Invalid position update" );
					}
					if ( colors ) {
						for ( let c = 0; c < 4; c++ ) {
							if ( !Number.isFinite( colors[i * 4 + c] ) ) throw Error( "Invalid position update" );
						}
					}
					if ( uvs ) {
						for ( let c = 0; c < 2; c++ ) {
							if ( !Number.isFinite( uvs[i * 2 + c] ) ) throw Error( "Invalid position update" );
						}
					}
				}
			}
			for ( const [start, end] of spans ) {
				for ( let i = start; i < end; i++ ) {
					for ( let c = 0; c < 3; c++ ) vertices[i * 14 + c] = positions[i * 3 + c]!;
					if ( colors ) { for ( let c = 0; c < 4; c++ ) vertices[i * 14 + 8 + c] = colors[i * 4 + c]!; }
					if ( uvs ) { for ( let c = 0; c < 2; c++ ) vertices[i * 14 + 6 + c] = uvs[i * 2 + c]!; }
				}
				const offset = start * 56;
				writeStream(
					draw.vertices,
					((draw.baseVertex ?? 0) + base) * VERTEX_BYTES + offset,
					vertices.subarray( start * 14, end * 14 )
				);
			}
		},
		/*
		================
		writeClothStream

		Writes entries of a cloth draw's stream (CLOTH_STREAM_WORDS words a
		slot, cloth-stream.ts) from slot first on, after the skin entries and
		the slot map. Only an upload with a clothStream has a stream.
		================
		*/
		writeClothStream( draw: GeometryDraw, first: number, entries: Uint32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( !meta.clothBase ) throw Error( "Geometry was uploaded without a cloth stream" );
			if (
				!Number.isInteger( first ) || first < 0 || entries.length % CLOTH_STREAM_WORDS ||
				first + entries.length / CLOTH_STREAM_WORDS > meta.clothSlots
			) throw Error( "Invalid cloth stream write" );
			// The writer copies bytes, so the words travel as they are.
			const words = new Float32Array( entries.buffer, entries.byteOffset, entries.length );
			writeStream( meta.skin, (2 * meta.clothBase + first) * CLOTH_STREAM_WORDS * 4, words, meta.cpuUploads );
		},
		/*
		================
		writeVertices
		================
		*/
		writeVertices( draw: GeometryDraw, base: number, vertices: Float32Array ) {
			const meta = metadata.get( draw ), mirror = meta?.vertices;
			if ( !mirror ) throw Error( "Geometry was uploaded without dynamicVertices" );
			if (
				!Number.isInteger( base ) || base < 0 || vertices.length % 14 ||
				base * 14 + vertices.length > mirror.length
			) throw Error( "Invalid vertex write" );
			mirror.set( vertices, base * 14 );
			writeStream(
				draw.vertices,
				((draw.baseVertex ?? 0) + base) * VERTEX_BYTES,
				mirror.subarray( base * 14, base * 14 + vertices.length )
			);
		},
		/*
		================
		dropMirror

		The mirror is the stream the draw was uploaded from: a settled scene's
		terrain gives it up so the scene's packed arena can be collected.
		================
		*/
		dropMirror( draw: GeometryDraw ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Stale geometry handle" );
			meta.vertices = undefined;
		},
		/*
		================
		streamVertices

		Writes a packed stream at vertex base with no CPU mirror: the caller
		keeps the stream (a terrain layer member's Geometry.vertices). Bounds
		and stride are checked as writeVertices checks them; the upload arena
		copies the bytes at admission, so the caller may change them after.
		================
		*/
		streamVertices( draw: GeometryDraw, base: number, vertices: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Stale geometry handle" );
			if (
				!Number.isInteger( base ) || base < 0 || vertices.length % 14 ||
				base * 14 + vertices.length > meta.vertexFloats
			) throw Error( "Invalid vertex write" );
			writeStream( draw.vertices, ((draw.baseVertex ?? 0) + base) * VERTEX_BYTES, vertices );
		},
		/*
		================
		updateInstances
		================
		*/
		updateInstances(
			draw: GeometryDraw,
			instances: Float32Array,
			opacity?: Float32Array,
			appearance?: Float32Array,
			pointLights?: Float32Array,
			paletteOffsets?: Uint32Array
		): GeometryDraw {
			const gpu = current(), buffers = geometryBuffers.get( draw );
			if ( !buffers ) {
				throw new Error( "Stale geometry handle" );
			}
			const meta = metadata.get( draw )!;
			if ( meta.palette ) validatePaletteOffsets( paletteOffsets, meta.boneBytes, meta.jointMaximum );
			else if ( paletteOffsets ) throw Error( "Palette offsets require shared bone storage" );
			const count = instances.length / 16,
				packed = markCloth(
					packInstances( instances, opacity, appearance, pointLights, paletteOffsets ),
					meta.clothBase
				);
			if ( count <= draw.instanceCapacity ) {
				// As with position/index/bone writes, the device's uncaptured
				// error listener owns write failures. Scopes are for allocation.
				if ( packed.byteLength ) {
					writeStream( meta.instances.buffer, meta.instances.offset, packed, meta.cpuUploads );
				}
				DeviceDraw.select( meta.selection, draw.count, count );
				return draw;
			}
			gpu.pushErrorScope( "validation" );
			try {
				if ( count > draw.instanceCapacity ) {
					const capacity = instanceCapacity( count ),
						storage = instanceStore( capacity * 160, meta.stores.owned );
					// The row states grow with the instances; the owner rewrites
					// them after the instances (updateRowStates).
					let rows: BufferSlice | undefined;
					try {
						rows = meta.stores.rowStates ? rowStateStore( capacity ) : undefined;
						writeStream( storage.binding.buffer, storage.binding.offset, packed, meta.cpuUploads );
						DeviceDraw.rebind(
							meta.selection,
							geometryBinding(
								meta.uniform,
								storage.binding,
								meta.material,
								rows?.binding ?? meta.rowStates,
								draw.pipeline,
								meta.clampedSampling,
								meta.image,
								meta.skin,
								meta.bones,
								meta.environmentImage
							),
							capacity
						);
						shadows?.forget( draw );
						sunShadows?.forget( draw );
						// The frame's recorded passes may still read the old storage.
						retire( meta.stores.instances );
						meta.stores.instances = storage;
						meta.instances = storage.binding;
						if ( rows ) {
							retire( meta.stores.rowStates! );
							meta.stores.rowStates = rows;
							meta.rowStates = rows.binding;
						}
					} catch ( error ) {
						retire( storage );
						if ( rows ) retire( rows );
						throw error;
					}
				}
				DeviceDraw.select( meta.selection, draw.count, count );
				return draw;
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) {
						fail( error.message );
					}
				} ).catch( fail );
			}
		},
		/*
		================
		updateTextureFactor

		The draw's D3DRS_TEXTUREFACTOR (rgba in [0, 1]), as a material
		modifier's pulse sets it each tick.
		================
		*/
		updateTextureFactor( draw: GeometryDraw, rgba: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( rgba.length !== 4 || !rgba.every( v => Number.isFinite( v ) && v >= 0 && v <= 1 ) ) {
				throw Error( "Invalid texture factor" );
			}
			writeMaterial( meta, meta.textureFactorOffset, rgba );
		},
		/*
		================
		updateMaterialColors
		================
		*/
		updateMaterialColors( draw: GeometryDraw, rgb: Float32Array, flags: number ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( rgb.length !== 3 || !rgb.every( Number.isFinite ) ) throw Error( "Invalid material colors" );
			if ( flags & 2 ) writeMaterial( meta, 0, rgb );
			if ( flags & 1 ) writeMaterial( meta, 128, rgb );
		},
		/*
		================
		updateRowStates

		A rowStates draw's row states from row 0, ROW_STATE_FLOATS a row,
		into the staged mirror; the GPU sees them after the next flush.
		================
		*/
		updateRowStates( draw: GeometryDraw, states: Float32Array ) {
			const meta = metadata.get( draw ), store = meta?.stores.rowStates;
			if ( !store ) throw Error( "Unknown row-state draw" );
			let valid = states.length % ROW_STATE_FLOATS === 0 && states.byteLength <= store.binding.size;
			for ( let i = 0; valid && i < states.length; i++ ) valid = Number.isFinite( states[i] );
			if ( !valid ) throw Error( "Invalid row states" );
			rowStateSlabs.write( store, states, states.byteLength );
		},
		/*
		================
		updateEquipmentGlow
		================
		*/
		updateEquipmentGlow(
			draw: GeometryDraw,
			color: Float32Array,
			uv: Float32Array,
			gain: number,
			alphaTest: boolean,
			enabled: boolean
		) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if (
				color.length !== 3 || uv.length !== 2 || !color.every( Number.isFinite ) ||
				!uv.every( Number.isFinite ) ||
				gain !== 1 && gain !== 2
			) throw Error( "Invalid equipment glow uniforms" );
			glowUniform.set( color );
			glowUniform[3] = enabled ? gain : 0;
			glowUniform.set( uv, 4 );
			glowUniform[6] = alphaTest ? 1 : 0;
			glowUniform[7] = 0;
			writeMaterial( meta, 192, glowUniform );
		},
		/*
		================
		updateTextureTransform
		================
		*/
		updateTextureTransform( draw: GeometryDraw, matrix: Float32Array ) {
			const meta = metadata.get( draw );
			if ( !meta ) throw Error( "Unknown geometry draw" );
			if ( matrix.length !== 8 || !matrix.every( Number.isFinite ) ) throw Error( "Invalid texture transform" );
			writeMaterial( meta, 144, matrix );
		},
		/*
		================
		updateTransform
		================
		*/
		updateTransform( draw: GeometryDraw, transform: Float32Array ) {
			const gpu = current(), meta = metadata.get( draw );
			if ( !meta ) {
				throw new Error( "Stale geometry handle" );
			}
			// A world draw binds the shared world uniform: it has no transform
			// of its own, and a write to one was never read.
			if ( !meta.stores.transform ) return;
			gpu.queue.writeBuffer(
				meta.uniform.buffer,
				meta.uniform.offset,
				transform.buffer as ArrayBuffer,
				transform.byteOffset,
				transform.byteLength
			);
		},
		/*
		================
		upload
		================
		*/
		upload( data: Geometry, image?: ImageDraw, paletteOffsets?: Uint32Array, environmentImage?: ImageDraw ) {
			const gpu = current(), buffers: GPUBuffer[] = [], slices: BufferSlice[] = [];
			let palette: SharedPalette | undefined, mesh: SharedMesh | undefined;
			gpu.pushErrorScope( "validation" );
			/*
			================
			slice

			A slab range holding data, written in place.
			================
			*/
			const slice = ( store: BufferSlice, data: Float32Array | Uint32Array ) => {
				slices.push( store );
				gpu.queue.writeBuffer(
					store.binding.buffer,
					store.binding.offset,
					data.buffer as ArrayBuffer,
					data.byteOffset,
					data.byteLength
				);
				return store;
			};
			/*
			================
			dynamicSlice

			A dynamic material's range: its every byte goes through the staged
			mirror, which flushMaterials sends before the next frame submit.
			================
			*/
			const dynamicSlice = ( store: BufferSlice, data: Float32Array ) => {
				slices.push( store );
				dynamicMaterialSlabs.write( store, data, data.byteLength );
				return store;
			};
			/*
			================
			buffer
			================
			*/
			const buffer = ( label: string, data: Float32Array | Uint32Array, usage: number ) => {
				const result = gpu.createBuffer( {
					label,
					size: data.byteLength,
					usage: usage | GPUBufferUsage.COPY_DST
				} );
				buffers.push( result );
				gpu.queue.writeBuffer( result, 0, data.buffer as ArrayBuffer, data.byteOffset, data.byteLength );
				return result;
			};
			try {
				// A skinned shader reads its per-vertex skin table by vertex_index,
				// which a baseVertex would shift: skinned streams keep buffers of
				// their own, one copy shared by every draw of an immutable mesh.
				const skinned = !!(data.joints && data.weights && data.bones);
				// A cloth stream shares the mesh's vertices and indices: the owner
				// writes the stream into the draw's own skin table, never into them.
				mesh = skinned && !data.dynamicVertices ? sharedMesh( gpu, data ) : undefined;
				// The asset worker packs terrain (Geometry.vertices); pack anything else here.
				const interleaved = mesh ?
					undefined :
					data.vertices?.length === data.positions.length / 3 * 14 ?
					data.vertices :
					packGeometryVertices( data );
				const clothBase = data.clothStream ? data.positions.length / 3 : 0,
					clothSlots = data.clothStream?.length ?? 0,
					vertexStore = skinned ?
						undefined :
						slice( vertexSlabs.allocate( interleaved!.byteLength ), interleaved! ),
					indexStore = skinned ?
						undefined :
						slice( indexSlabs.allocate( data.indices.byteLength ), data.indices ),
					vertices = mesh?.vertices ?? vertexStore?.binding.buffer ??
						buffer( "geometry-vertices", interleaved!, GPUBufferUsage.VERTEX ),
					indices = mesh?.indices ?? indexStore?.binding.buffer ??
						buffer( "geometry-indices", data.indices, GPUBufferUsage.INDEX ),
					transform = data.world ? undefined : slice( transformSlabs.allocate( 64 ), data.transform ),
					uniform = transform?.binding ?? worldBinding;
				const instances = data.instances ??
						new Float32Array( [ 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 ] ),
					count = instances.length / 16,
					capacity = instanceCapacity( count ),
					packed = markCloth(
						packInstances( instances, undefined, undefined, undefined, paletteOffsets ),
						clothBase
					),
					// Only owned (unshared) bone palettes can present particles.
					owned = !!(data.joints && data.weights && data.bones && !paletteOffsets);
				const storage = instanceStore( capacity * 160, owned );
				slices.push( storage );
				const rowStates = data.rowStates ? rowStateStore( capacity ) : undefined;
				if ( rowStates ) slices.push( rowStates );
				if ( instances.byteLength ) {
					gpu.queue.writeBuffer(
						storage.binding.buffer,
						storage.binding.offset,
						packed.buffer as ArrayBuffer,
						packed.byteOffset,
						packed.byteLength
					);
				}
				let skinBuffer = defaultSkin, boneBuffer = defaultBones;
				let jointMaximum = 0;
				if ( data.joints ) {
					for ( const joint of data.joints ) jointMaximum = Math.max( jointMaximum, joint );
				}
				// The stream lives after the skin entries of the draw's own skin
				// table; its palette may be the batch's shared one.
				if ( data.clothStream && (!skinned || data.dynamicVertices) ) {
					throw Error( "A cloth stream needs a skinned mesh" );
				}
				if ( data.clothStream && !validClothStreamSlots( data.clothStream, clothBase ) ) {
					throw Error( "Invalid cloth stream slots" );
				}
				if ( data.joints && data.weights && data.bones ) {
					if ( mesh && !clothBase ) skinBuffer = mesh.skin;
					else {
						// A cloth mesh's table, in entries of a skin entry's 8 words: its
						// V skin entries, then a slot map of one entry a vertex (word 0:
						// the vertex's stream slot, or CLOTH_NO_SLOT), then the stream's
						// entries, all zero: until the owner writes it, the GPU skins
						// every vertex.
						const vertices = data.joints.length / 4,
							entries = vertices + (clothBase ? vertices + clothSlots : 0),
							packed = new ArrayBuffer( entries * CLOTH_STREAM_WORDS * 4 ),
							joints = new Uint32Array( packed ),
							weights = new Float32Array( packed );
						for ( let v = 0; v < vertices; v++ ) {
							for ( let c = 0; c < 4; c++ ) {
								joints[v * 8 + c] = data.joints[v * 4 + c]!;
								weights[v * 8 + 4 + c] = data.weights[v * 4 + c]!;
							}
						}
						if ( clothBase ) {
							for ( let v = 0; v < vertices; v++ ) joints[(clothBase + v) * 8] = CLOTH_NO_SLOT;
							for ( let k = 0; k < clothSlots; k++ ) joints[(clothBase + data.clothStream![k]!) * 8] = k;
						}
						skinBuffer = buffer( "geometry-skin", joints, GPUBufferUsage.STORAGE );
					}
					if ( paletteOffsets ) {
						palette = sharedPalettes.get( data.bones );
						if ( !palette ) {
							const storage = gpu.createBuffer( {
								label: "geometry-palette",
								size: data.bones.byteLength,
								usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST
							} );
							palette = { source: data.bones, buffer: storage, refs: 0, revision: -1 };
							sharedPalettes.set( data.bones, palette );
						}
						palette.refs++;
						boneBuffer = palette.buffer;
					} else {boneBuffer = buffer(
							"geometry-bones",
							data.bones,
							GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC
						);}
				}
				if ( paletteOffsets ) {
					if ( !palette ) throw Error( "Palette offsets require skinned geometry" );
					validatePaletteOffsets( paletteOffsets, boneBuffer.size, jointMaximum );
				}
				const mat = data.material;
				if ( mat?.environmentReflection && !environmentImage ) {
					throw Error( "Reflective material requires its owned environment texture" );
				}
				if ( mat?.objectLightmap && (!environmentImage || !data.maskUVs && !data.vertices) ) {
					throw Error( "Lightmapped material requires its lightmap texture and UVs" );
				}
				const materialData = new Float32Array( [
						...(mat?.color ?? [ 0.75, 0.78, 0.82, 1 ]),
						mat?.alphaCutoff ?? 0,
						mat?.unlit === false ? 0 : 1,
						mat?.terrain ? 1 : 0,
						mat?.stageFactor ?? 1,
						data.bones ?
							(mat?.sharedPose ?
								-data.bones.length / 16 :
								data.bones.length / 16 / Math.max( 1, count )) :
							0,
						mat?.lightmap ? 1 : 0,
						mat?.water ? 1 : 0,
						mat?.sky ?? 0,
						1,
						1,
						0,
						0,
						mat?.objectLight ?? 1,
						mat?.objectLight !== undefined ? 1 : 0,
						mat?.instanceFade ? 2 : mat?.objectFade ? 1 : 0,
						mat?.fadeAlphaOnly ? 1 : 0,
						mat?.fogDisabled ? 1 : 0,
						mat?.fog ? (mat.fog.linear ? LOCAL_FOG_LINEAR : LOCAL_FOG_SHADER) : 0,
						mat?.textureAlpha === false ? 1 : 0,
						mat?.instanceMaterialTint ? 1 : 0,
						...(mat?.fog ?
							[
								((mat.fog.color >>> 16) & 255) / 255,
								((mat.fog.color >>> 8) & 255) / 255,
								(mat.fog.color & 255) / 255,
								mat.fog.nearPlane,
								mat.fog.farPlane,
								mat.fog.intensity,
								...dungeonHeightFog( mat.fog ).slice( 0, 2 )
							] :
							[ 0, 0, 0, 0, 0, 0, 0, 0 ]),
						...(mat?.ambient ?? [ 1, 1, 1 ]),
						mat?.surfaceAlpha ? 1 : 0,
						1,
						0,
						0,
						0,
						0,
						1,
						0,
						0,
						mat?.environmentReflection ?? 0,
						mat?.alphaCompare ?? 7,
						// Port-only, not native: reflection.z admits world-space
						// receivers; portrait/doll coordinates belong to another scene.
						data.world === true ? 1 : 0,
						mat?.water && environmentImage ? 1 : 0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						0,
						...packTextureStage( mat?.textureStage ),
						// The stage policy: x, DIFFUSE is the BSR shader's oD0; y, the
						// draw blends nothing, so its surviving pixels are opaque; z, stage 1
						// is the object's lightmap (binding 8 at the second UV set); w, the
						// slope of the block's height fog.
						mat?.shaderDiffuse ? 1 : 0,
						geometryPipelineState( mat ).blend ? 0 : 1,
						mat?.objectLightmap ? 1 : 0,
						mat?.fog ? dungeonHeightFog( mat.fog )[2] : 0,
						// D3DRS_TEXTUREFACTOR, read by a stage's TFACTOR argument.
						...(mat?.textureFactor ?? [ 1, 1, 1, 1 ])
					] ),
					material = data.dynamicMaterial ?
						dynamicSlice( dynamicMaterialSlabs.allocate( materialData.byteLength ), materialData ) :
						slice( materialSlabs.allocate( materialData.byteLength ), materialData );
				const selected = pipelines(
						geometryPipelineState( mat ),
						{ sceneFloat: floatScene, msaa: msaaScene },
						!!data.rowStates
					),
					clampedSampling = !!(mat?.lightmap || mat?.decal) && !mat?.groundDecal;
				const binding = geometryBinding(
					uniform,
					storage.binding,
					material.binding,
					rowStates?.binding,
					selected,
					clampedSampling,
					image,
					skinBuffer,
					boneBuffer,
					environmentImage
				);
				const draw = new DeviceDraw(
					{
						deferredParticle: mat?.deferredParticle,
						blended: data.material?.sky ? false : data.material?.blend ?? false,
						pipeline: selected,
						vertices,
						indices,
						baseVertex: (vertexStore?.binding.offset ?? 0) / VERTEX_BYTES,
						firstIndex: (indexStore?.binding.offset ?? 0) / INDEX_BYTES,
						count: data.indices.length
					},
					binding,
					capacity,
					count
				);
				// The draw binds its images until release; a released image is
				// refused here, before the draw is recorded.
				if ( image ) images.acquire( image );
				if ( environmentImage ) {
					try {
						images.acquire( environmentImage );
					} catch ( error ) {
						if ( image ) images.drop( image );
						throw error;
					}
				}
				geometryBuffers.set( draw, buffers );
				if ( mesh ) drawMeshes.set( draw, mesh );
				metadata.set( draw, {
					water: !!mat?.water,
					state: geometryPipelineState( mat ),
					uniform,
					material: material.binding,
					instances: storage.binding,
					rowStates: rowStates?.binding,
					stores: {
						transform,
						material,
						instances: storage,
						rowStates,
						owned,
						vertices: vertexStore,
						indices: indexStore
					},
					image,
					environmentImage,
					...(data.dynamicVertices ? { vertices: interleaved } : {}),
					vertexFloats: data.positions.length / 3 * 14,
					skin: skinBuffer,
					bones: boneBuffer,
					clothBase,
					clothSlots,
					boneBytes: boneBuffer.size,
					cpuUploads: !particles || boneBuffer === defaultBones || !!palette,
					indexBytes: data.indices.byteLength,
					palette,
					jointMaximum,
					clampedSampling,
					textureFactorOffset: materialData.byteLength - 16,
					dynamicMaterial: !!data.dynamicMaterial,
					selection: draw
				} );
				return draw;
			} catch ( error ) {
				releasePalette( palette );
				releaseMesh( mesh );
				for ( const item of buffers ) {
					item.destroy();
				}
				for ( const item of slices ) retire( item );
				throw error;
			} finally {
				gpu.popErrorScope().then( error => {
					if ( error ) {
						fail( error.message );
					}
				} ).catch( fail );
			}
		},
		/*
		================
		releasedDraw
		================
		*/
		releasedDraw( draw: GeometryDraw ) {
			const released = releasedDraws.get( draw );
			return released ? { atMs: released.atMs, stack: released.error.stack ?? "" } : undefined;
		},
		/*
		================
		release
		================
		*/
		release( draw: GeometryDraw ) {
			// The releasing stack names the owner if another one still lists it.
			// Keep the first record: a repeated release must not hide the original.
			if ( !releasedDraws.has( draw ) ) {
				releasedDraws.set( draw, {
					atMs: performance.now(),
					// Preserve the releasing call site, but format its stack only if a
					// stale draw is actually reported. Region retirement releases many.
					error: new Error( "geometry release" )
				} );
			}
			shadows?.forget( draw );
			sunShadows?.forget( draw );
			for ( const buffer of geometryBuffers.get( draw ) ?? [] ) {
				retire( buffer );
			}
			const meta = metadata.get( draw );
			if ( meta ) {
				if ( meta.stores.transform ) retire( meta.stores.transform );
				if ( meta.stores.vertices ) retire( meta.stores.vertices );
				if ( meta.stores.indices ) retire( meta.stores.indices );
				retire( meta.stores.material );
				retire( meta.stores.instances );
				if ( meta.stores.rowStates ) retire( meta.stores.rowStates );
			}
			releasePalette( meta?.palette );
			releaseMesh( drawMeshes.get( draw ) );
			drawMeshes.delete( draw );
			if ( meta?.image ) images.drop( meta.image );
			if ( meta?.environmentImage ) images.drop( meta.environmentImage );
			particles?.release( draw );
			geometryBuffers.delete( draw );
			metadata.delete( draw );
		}
	} );
	return {
		/*
		================
		beginFrame
		================
		*/
		beginFrame() {
			uploads.beginFrame();
			particles?.beginFrame();
		},
		/*
		================
		uploads

		The submission arena; the device submits each frame through it so CPU
		streams upload before the completed frame's compute and render work.
		================
		*/
		uploads,
		/*
		================
		flushMaterials

		The dynamic materials' and the row states' updates in one write a
		slab. The device calls it before every frame submit, the deferred
		frame's included, so the writes precede the commands that read them;
		endFrame calls it too.
		================
		*/
		flushMaterials() {
			dynamicMaterialSlabs.flush();
			rowStateSlabs.flush();
		},
		/*
		================
		endFrame

		An abandoned frame still commits its material updates, as the direct
		writes did, while retirement still holds the ranges released this
		frame.
		================
		*/
		endFrame() {
			try {
				dynamicMaterialSlabs.flush();
				rowStateSlabs.flush();
			} finally {
				uploads.endFrame();
			}
		},
		commands,
		ready: Promise.all( [ animation?.ready, particles?.ready ] ),
		/*
		================
		prepare
		================
		*/
		prepare( encoder: GPUCommandEncoder, timing?: import("../internal/gpu-contract").GpuTimingFrame ) {
			animation?.encode( encoder, timing );
			particles?.encode( encoder, timing );
			shadows?.encode( encoder, timing );
		},
		/*
		================
		textureOptions
		================
		*/
		textureOptions( nextFiltered: boolean, nextDetail: number, nextAnisotropic = anisotropic ) {
			if ( filtered === nextFiltered && detail === nextDetail && anisotropic === nextAnisotropic ) return;
			if ( !Number.isInteger( nextDetail ) || nextDetail < 0 || nextDetail > 2 ) {
				throw Error( "Invalid texture detail" );
			}
			filtered = nextFiltered;
			detail = nextDetail;
			anisotropic = nextAnisotropic;
			if ( !sampling ) throw Error( "Texture settings capability unavailable" );
			worldSampler = sampling( filtered, detail, anisotropic );
			if ( lightmapSampling ) lightmapSampler = lightmapSampling( anisotropic );
			for ( const [draw, meta] of metadata ) {
				DeviceDraw.rebind(
					meta.selection,
					geometryBinding(
						meta.uniform,
						meta.instances,
						meta.material,
						meta.rowStates,
						draw.pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage
					)
				);
			}
		},
		/*
		================
		worldView
		================
		*/
		worldView( transform: Float32Array ) {
			current().queue.writeBuffer( worldUniform, 0, transform.buffer as ArrayBuffer, transform.byteOffset, 64 );
		},
		/*
		================
		sunShadow

		The experimental cascade owner: the renderer prepares it per frame
		(eye, light) and encodes its caster pass before the main pass.
		================
		*/
		sunShadow() {
			return ensureSunShadow();
		},
		/*
		================
		sceneBindings

		The scene surface flip: every live draw moves to the pipeline of the
		current scene format and sample count (a new implicit layout, so a
		new binding), and the water mirror reformats to match the reflected
		pass. Nothing renders between the flip and the walk's completion.
		================
		*/
		sceneBindings( sceneFloat: boolean, sceneMsaa = false ) {
			if ( floatScene === sceneFloat && msaaScene === sceneMsaa ) return;
			floatScene = sceneFloat;
			msaaScene = sceneMsaa;
			waterReflection.reformat( sceneFloat ? "rgba16float" : format, sceneMsaa ? MSAA_SAMPLE_COUNT : 1 );
			for ( const [draw, meta] of metadata ) {
				const pipeline = pipelines( meta.state, { sceneFloat, msaa: sceneMsaa }, !!meta.rowStates );
				DeviceDraw.repipeline(
					meta.selection,
					pipeline,
					geometryBinding(
						meta.uniform,
						meta.instances,
						meta.material,
						meta.rowStates,
						pipeline,
						meta.clampedSampling,
						meta.image,
						meta.skin,
						meta.bones,
						meta.environmentImage
					)
				);
			}
		},
		/*
		================
		refreshShadowBindings

		The sun cascade toggled: bindings 13/14 now name another view, so
		every draw rebinds against its current pipeline.
		================
		*/
		refreshShadowBindings( enabled: boolean ) {
			if ( shadowEnabled === enabled ) return;
			// Bind the selected target before prepare() activates the first frame.
			shadowEnabled = enabled;
			rebindEveryDraw();
		},
		/*
		================
		refreshAmbientOcclusion

		The ambient occlusion owner's per-frame answer (experimental,
		#2094): null while the stage is off or the scene has no size.
		The swap travels the same identity guard as the cascade's: the
		owner's blurred view is stable per scene size, so bindings stay
		valid across frames and only an enable, disable or resize
		refreshes them; the frame's render bundles ride on that
		stability. A changed view rebinds every draw through the one
		geometryBinding funnel.
		================
		*/
		refreshAmbientOcclusion( view: GPUTextureView | null ) {
			const next = view ?? aoDummyView;
			if ( next === aoBoundView ) {
				return;
			}
			aoBoundView = next;
			rebindEveryDraw();
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			uploads.dispose();
			waterReflection.dispose();
			shadows?.dispose();
			sunShadows?.dispose();
			animation?.dispose();
			particles?.dispose();
			for ( const buffers of geometryBuffers.values() ) {
				for ( const buffer of buffers ) {
					buffer.destroy();
				}
			}
			for ( const palette of sharedPalettes.values() ) palette.buffer.destroy();
			sharedPalettes.clear();
			for ( const list of sharedMeshes.values() ) {
				for ( const mesh of list ) {
					mesh.vertices.destroy();
					mesh.indices.destroy();
					mesh.skin.destroy();
				}
			}
			sharedMeshes.clear();
			drawMeshes.clear();
			transformSlabs.dispose();
			materialSlabs.dispose();
			dynamicMaterialSlabs.dispose();
			rowStateSlabs.dispose();
			instanceSlabs.dispose();
			vertexSlabs.dispose();
			indexSlabs.dispose();
			for ( const buffer of ownedInstances ) buffer.destroy();
			ownedInstances.clear();
			geometryBuffers.clear();
			metadata.clear();
			worldUniform.destroy();
			white.destroy();
			shadowDummy.destroy();
			defaultSkin.destroy();
			defaultBones.destroy();
		}
	};
}
