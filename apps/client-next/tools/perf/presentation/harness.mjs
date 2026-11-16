/*
===========================================================================

harness.mjs - one tree's character presentation, replayed frame by frame

createPresentationHarness( root, session ) builds the character
presentation of the checkout at ROOT the way runtime.ts builds it: the
world projection (runtime/presentation), the shared presentation RNG, the
real CPU side of the characters renderer, and an asset owner that serves
the shared published tree (or a scene's own sources, options.source).
Each step( index ) applies the session frame's world batch and calls the
presentation in runtime.ts's frame order, then returns everything the
presentation handed to the rest of the client that frame, encoded at
call time.

Every module comes from ROOT (its loader, its presentation-fixture rig,
its crowd-capture encoder, its sources, its generated-root resolver), so
one checkout of this tool replays any tree. Nothing reads the wall clock;
Math.random, which the trade-appearance lookup falls back on, is pinned to
a seeded stream for the harness's lifetime.

What is not the client: the GPU (no-op commands), world scenery (none),
terrain (one session-wide sound surface), the local mover (always idle)
and dungeon room culling (outdoors). Asset results land on the frame
after their request, in request order, from the loose published files
(or the scene's bytes) decoded by ROOT's own worker decoders.

===========================================================================
*/
import { readFileSync, existsSync, statSync } from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

// assets.ts REQUEST_CAPACITY: the asset owner's foreground slots.
const REQUEST_CAPACITY = 4;
// assets.ts request()'s default byte limit.
const DEFAULT_REQUEST_LIMIT = 16 << 20;
const NATIVE_TEXTURE_MIME = "application/x-sro-texture";
const PNG_SIGNATURE = 0x89504e47;
// The view the renderer's CPU frame culls and orients billboards with:
// a 16:9 frame through the session camera (eye and target).
const CAMERA_LENS = { fov: 0.9, near: 1, far: 5000 };
const VIEW_ASPECT = 16 / 9;
// The renderer's Experimental defaults (experimental-options.ts): only an
// explicit false turns these off, vHidesCompanions only an explicit true on.
const EXPERIMENTAL_DEFAULTS = {
	enchantSparks: true,
	sharedActorRoom: true,
	vHidesCompanions: false,
	dungeonCharacterPvs: true,
	silentWingFlight: true
};
// The renderer port calls that leave the presentation, and how each is
// recorded. A model or clip is recorded by what the presentation chose
// (its id, clip names and durations), not by the bytes this harness served.
const RENDERER_SINKS = {
	setCharacterActors: ( actors, portraits = [] ) => [ actors, portraits ],
	setCharacterAssembly: ( id, base, parts ) => [ id, base, parts ],
	setCharacterModel: ( id, model, images ) => [
		id,
		model.clips.map( clip => [ clip.name, clip.duration ] ),
		images.length
	],
	setCharacterAnimation: ( body, name, source ) => [ body, name, source.duration ],
	// The renderer reads the list as a set and ignores a set equal to the
	// one it holds (retainedSet): recorded is the set it then holds, in
	// its order, on the calls that change it.
	retainCharacterModels: ids => [ [ ...new Set( ids ) ] ],
	setFootprints: rows => [ rows ],
	setSunDropMarkers: rows => [ rows ]
};

/*
================
GEOMETRY

Every GeometryCommands member the renderer's CPU frame calls, doing
nothing (internal/gpu-contract.ts). The optional GPU skinning members are
absent, so bones are evaluated on the CPU, as without GPU animation.
================
*/
const GEOMETRY = Object.freeze( {
	hdrPreview: draws => draws,
	singleSampled: draws => draws,
	waterReflection: () => undefined,
	updateMaterialColors() {},
	updateTextureFactor() {},
	updateEquipmentGlow() {},
	updateTextureTransform() {},
	updateBones: () => 0,
	presentParticles() {},
	updateIndices() {},
	updatePositions() {},
	writeVertices() {},
	writeClothStream() {},
	streamVertices() {},
	dropMirror() {},
	updateInstances: draw => draw,
	updateTransform() {},
	upload: () => ({}),
	release() {}
} );
const IMAGES = Object.freeze( { upload: () => ({}), release() {} } );

/*
================
rootModule

A module of the tree under replay, by its path from the repository root.
================
*/
function rootModule( root, relative ) {
	const file = path.join( root, relative );
	if ( !existsSync( file ) ) throw Error( `${relative} is missing from ${root}` );
	return import( pathToFileURL( file ).href );
}

/*
================
seededRandom

mulberry32: Math.random's stand-in, so a draw the presentation takes
from it repeats in every run and every tree.
================
*/
function seededRandom( seed ) {
	let state = seed >>> 0;
	return () => {
		state = (state + 0x6d2b79f5) >>> 0;
		let t = state;
		t = Math.imul( t ^ (t >>> 15), t | 1 );
		t ^= t + Math.imul( t ^ (t >>> 7), t | 61 );
		return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
	};
}

/*
================
imageStandIn

What the asset worker hands the page for one embedded image: a native
texture decodes as the worker decodes it; a PNG becomes its dimensions
(the page never sees a PNG's pixels, only its ImageBitmap's size).
================
*/
function imageStandIn( image, decodeNativeTexture ) {
	if ( image.mime === NATIVE_TEXTURE_MIME ) return decodeNativeTexture( image.bytes );
	const view = new DataView( image.bytes.buffer, image.bytes.byteOffset, image.bytes.byteLength );
	if ( image.bytes.byteLength < 24 || view.getUint32( 0 ) !== PNG_SIGNATURE ) throw Error( "Unsupported image" );
	return { width: view.getUint32( 16 ), height: view.getUint32( 20 ), close() {} };
}

/*
================
publishedFiles

The default source: a published path's bytes from the loose files under
publicRoot, null when no file is there.
================
*/
function publishedFiles( publicRoot ) {
	return pathname => {
		const file = path.join( publicRoot, pathname );
		return existsSync( file ) && statSync( file ).isFile() ? new Uint8Array( readFileSync( file ) ) : null;
	};
}

/*
================
createPublishedAssets

The AssetOwner over a source of published bytes (pathname -> bytes or
null). A request takes one of the four slots and completes on the next
frame (take answers null until then); a cancel of a pending load holds
its slot until the next frame, as the worker's acknowledgement does.
Bytes are read once per path; models and catalogs are decoded on every
delivery, as the worker decodes them.
================
*/
function createPublishedAssets( source, decoders, record ) {
	const jobs = new Map(), files = new Map();
	let nextId = 0, frame = 0, programsParsed = false;
	/*
	================
	read
	================
	*/
	function read( pathname ) {
		let bytes = files.get( pathname );
		if ( bytes === undefined ) {
			bytes = source( pathname ) ?? null;
			files.set( pathname, bytes );
		}
		return bytes;
	}
	/*
	================
	character

	The loader's character branch (loader.ts): decode, then images as
	the page receives them.
	================
	*/
	function character( source ) {
		const images = source.images.map( image => imageStandIn( image, decoders.decodeNativeTexture ) );
		return {
			kind: "character",
			model: { ...source, images: images.map( image => ({ width: image.width, height: image.height }) ) },
			images
		};
	}
	/*
	================
	resolve
	================
	*/
	function resolve( job ) {
		const url = new URL( job.url ), pathname = decodeURIComponent( url.pathname );
		if ( !pathname.startsWith( "/assets/" ) || pathname.includes( ".." ) ) {
			return { kind: "error", error: "Invalid asset URL" };
		}
		const bytes = read( pathname );
		if ( !bytes ) return { kind: "error", error: `Asset absent: ${pathname}`, absent: true };
		if ( bytes.byteLength > job.limit ) return { kind: "error", error: `Asset exceeds its limit: ${pathname}` };
		try {
			if ( job.decode === undefined ) return { kind: "bytes", buffer: bytes.slice().buffer };
			if ( job.decode === "effects" ) return { kind: "effects", catalog: decoders.effects.decode( bytes ) };
			if ( job.decode === "character" ) {
				return character( decoders.models.character( decoders.models.decode( bytes.slice() ) ) );
			}
			if ( job.decode === "effect" ) {
				// The worker parses the program catalog once and keeps it.
				const decoded = decoders.effects.model(
					programsParsed ? new Uint8Array( 0 ) : bytes,
					decodeURIComponent( url.hash.slice( 1 ) )
				);
				programsParsed = true;
				const raw = decoded.imagePaths.map( image => {
					const pixels = read( image );
					if ( !pixels ) throw Error( `Effect image absent: ${image}` );
					return { bytes: pixels, mime: "image/png" };
				} );
				return character( { ...decoded.model, images: raw } );
			}
			return { kind: "error", error: `Unsupported decode ${job.decode}` };
		} catch ( error ) {
			return { kind: "error", error: String( error ) };
		}
	}
	return {
		/*
		================
		beginFrame

		A new frame: last frame's loads complete and its cancellations
		are acknowledged.
		================
		*/
		beginFrame( index ) {
			frame = index;
			for ( const [id, job] of jobs ) if ( job.cancelled && job.frame < frame ) jobs.delete( id );
		},
		available: () => REQUEST_CAPACITY - jobs.size,
		health: () => ({ phase: "running" }),
		progress: () => null,
		/*
		================
		request
		================
		*/
		request( url, limit = DEFAULT_REQUEST_LIMIT, decode ) {
			if ( jobs.size >= REQUEST_CAPACITY ) throw Error( "Asset request exceeds budget" );
			const id = ++nextId;
			jobs.set( id, { url: new URL( url ).href, limit, decode, frame, cancelled: false } );
			record( "requests", [ url, limit, decode ] );
			return id;
		},
		/*
		================
		take
		================
		*/
		take( id ) {
			const job = jobs.get( id );
			if ( !job || job.cancelled || job.frame >= frame ) return null;
			jobs.delete( id );
			return resolve( job );
		},
		/*
		================
		cancel
		================
		*/
		cancel( id ) {
			const job = jobs.get( id );
			if ( !job || job.cancelled ) return;
			record( "cancels", [ job.url ] );
			if ( job.frame < frame ) jobs.delete( id );
			else job.cancelled = true;
		},
		textureFeatures() {},
		hdTextures() {},
		install() {},
		dispose() {
			jobs.clear();
			files.clear();
		}
	};
}

/*
================
createPresentationHarness

ROOT is a repository checkout; session is a parsed session object
(scenarios.mjs). options.due, when given, replaces the presentation's
appearance and motion rules (presentation-due.ts), so a run can present
every entity in full or count the rules' answers; options.actors, when
given, sees every published actor list as the renderer receives it.
options.source, when given, serves the published bytes in place of the
built tree (pathname, as "/assets/...", to a Uint8Array or null for an
absent file): a scene of its own needs no built assets. The harness owns
Math.random until dispose.
================
*/
export async function createPresentationHarness( root, session, options = {} ) {
	if ( session?.format !== "sro-presentation-session" || session.version !== 1 ) {
		throw Error( "Not a presentation session" );
	}
	const client = "apps/client-next/", src = client + "src/engine/";
	await rootModule( root, client + "tests/helpers/native-source-loader.mjs" );
	const { presentationRig } = await rootModule( root, client + "tests/helpers/presentation-fixture.mjs" );
	const { encodeCrowdValue } = await rootModule( root, client + "tools/perf/core/crowd-capture.mjs" );
	const { CLIENT_PUBLIC_ROOT } = await rootModule( root, "scripts/lib/generatedRoot.mjs" );
	const { createPresentation } = await rootModule( root, src + "runtime/presentation/presentation.ts" );
	const { createPresentationRandom } = await rootModule( root, src + "runtime/random/random.ts" );
	const { createModelDecoder } = await rootModule( root, src + "runtime/assets/worker/model/model.ts" );
	const { createEffectDecoder } = await rootModule( root, src + "runtime/assets/worker/effects/effects.ts" );
	const { decodeNativeTexture } = await rootModule( root, src + "foundation/assets/native-texture.ts" );
	const { worldAudioPosition } = await rootModule( root, src + "foundation/world/world-space.ts" );
	const { viewProjection } = await rootModule( root, src + "foundation/rendering/world-math.ts" );
	const settings = session.settings ?? {};
	const experimental = { ...EXPERIMENTAL_DEFAULTS, ...settings.experimental };
	const mathRandom = Math.random;
	Math.random = seededRandom( session.seed ^ 0x5bd1e995 );
	// The frame's outputs, grouped by kind; call order is kept within a kind.
	let out = {};
	/*
	================
	record
	================
	*/
	function record( kind, value ) {
		(out[kind] ??= []).push( encodeCrowdValue( value ) );
	}
	const random = createPresentationRandom( session.seed >>> 0 );
	const world = createPresentation();
	const decoders = { models: createModelDecoder(), effects: createEffectDecoder(), decodeNativeTexture };
	const assets = createPublishedAssets( options.source ?? publishedFiles( CLIENT_PUBLIC_ROOT ), decoders, record );
	// What the renderer's last frame left for the presentation to read.
	const view = { camera: null, listener: null };
	const missing = new Set();
	// runtime.ts passes the world projection itself as the health port;
	// its mutating calls are outputs of the presentation.
	const health = {
		impact( ...args ) {
			record( "health", [ "impact", ...args ] );
			return world.impact( ...args );
		},
		currentResult: world.currentResult,
		dead: world.dead,
		release( ...args ) {
			record( "health", [ "release", ...args ] );
			return world.release( ...args );
		},
		finishedCasts() {
			record( "health", [ "finishedCasts" ] );
			return world.finishedCasts();
		}
	};
	// The model set the renderer was last handed to retain.
	let retained = null;
	/*
	================
	retainedSet

	Whether a retainCharacterModels list changes the renderer's set (as
	characters.ts retain decides), taking it when it does.
	================
	*/
	function retainedSet( ids ) {
		const set = new Set( ids );
		if ( retained && retained.size === set.size && [ ...set ].every( id => retained.has( id ) ) ) return false;
		retained = set;
		return true;
	}
	const rig = presentationRig( {
		assets,
		random,
		rendererRandom: random,
		play: event => record( "sounds", event ),
		surface: () => settings.surface ?? undefined,
		health,
		soundEnabled: () => true,
		due: options.due,
		// renderer.ts's port, member for member, over the CPU module.
		port: characters => ({
			characterLocalMatrix: characters.localMatrix,
			characterSocket: characters.socket,
			setCharacterAnimation: characters.animation,
			setCharacterAssembly: characters.assembly,
			presentationNight: () => settings.night ?? true,
			presentationCamera: () => view.camera,
			audioListener: () => view.listener,
			enchantSparks: () => experimental.enchantSparks,
			sharedActorRoom: () => experimental.sharedActorRoom,
			vHidesCompanions: () => experimental.vHidesCompanions,
			silentWingFlight: () => experimental.silentWingFlight,
			dungeonCharacterPvs: () => experimental.dungeonCharacterPvs,
			dungeonBlockHides: () => false,
			scenery: () => null,
			setFootprints() {},
			setSunDropMarkers() {}
		}),
		wrap: port =>
			new Proxy( port, {
				get( target, key ) {
					const value = target[key];
					if ( typeof key !== "string" ) return value;
					if ( value === undefined ) missing.add( key );
					const sink = RENDERER_SINKS[key];
					if ( !sink || typeof value !== "function" ) return value;
					return ( ...args ) => {
						if ( key !== "retainCharacterModels" || retainedSet( args[0] ) ) record( key, sink( ...args ) );
						if ( key === "setCharacterActors" ) options.actors?.( ...args );
						return value.apply( target, args );
					};
				}
			} )
	} );
	const characters = rig.presentation;
	const frameWork = { level: () => 0, remaining: () => 2, spend() {} };
	let sequence = 0, camera = null, pitch, blindHeld = false;
	return {
		frames: session.frames.length,
		missing,
		/*
		================
		step

		One client frame, in runtime.ts's order: frame work, the world
		batch, the clock origin, feedback, effective HP, the mover, the
		presentation step, the reads the runtime makes after it, then the
		renderer's CPU frame, which publishes the camera and listener
		the next frame's presentation reads.
		================
		*/
		step( index ) {
			const frame = session.frames[index];
			out = {};
			assets.beginFrame( index );
			characters.frameWork( frameWork );
			if ( frame.batch ) {
				const batch = { sequence: ++sequence, events: frame.batch.events, spanMs: frame.batch.spanMs };
				if ( world.apply( batch ) ) characters.reset();
				characters.receiveLifecycle( batch.events );
			}
			if ( frame.originMs !== undefined ) characters.simulationOrigin( frame.originMs );
			if ( frame.camera !== undefined ) camera = frame.camera;
			if ( frame.pitch !== undefined ) pitch = frame.pitch;
			if ( frame.blindHeld !== undefined ) blindHeld = frame.blindHeld;
			characters.receiveFeedback( world.takeFeedback(), world.entities() );
			world.step( frame.simulationMs );
			characters.profile( undefined );
			characters.mallOutfit( null, null );
			const gameplay = world.gameplay(), localGid = gameplay?.localGid ?? 0;
			characters.ownMover(
				localGid,
				{ kind: "idle" },
				world.entities().find( entity => entity.gid === localGid )?.mountedOn || undefined
			);
			characters.step(
				world.entities(),
				gameplay,
				frame.nowMs / 1000,
				frame.simulationMs,
				pitch,
				undefined,
				null,
				false,
				settings.effectDetail ?? 2,
				blindHeld,
				settings.serverName,
				settings.normalFortressClothes ?? false
			);
			const reads = {
				mallPreview: characters.mallPreviewState(),
				eventRain: characters.eventRain(),
				cameraTarget: characters.cameraTarget(),
				cameraScripts: characters.takeCameraScripts(),
				ready: characters.ready( localGid ),
				entryReady: characters.entryReady(),
				error: characters.error(),
				orbGauge: characters.orbGauge(),
				damageText: characters.damageText(),
				degradations: characters.takePresentationDegradations(),
				drawnPose: characters.drawnPose( localGid )
			};
			rig.characters.prepare(
				GEOMETRY,
				IMAGES,
				camera?.regionId ?? 0,
				camera ?
					viewProjection(
						{ eye: [ camera.x, camera.y, camera.z ], target: camera.target, ...CAMERA_LENS },
						VIEW_ASPECT
					) :
					undefined,
				false,
				frame.nowMs / 1000,
				true,
				settings.night ?? true
			);
			view.camera = camera && { regionId: camera.regionId, x: camera.x, y: camera.y, z: camera.z };
			view.listener = camera ? { position: worldAudioPosition( camera ) } : null;
			return { frame: index, ...out, reads: encodeCrowdValue( reads ) };
		},
		/*
		================
		dispose
		================
		*/
		dispose() {
			try {
				characters.dispose();
				rig.characters.dispose( GEOMETRY, IMAGES );
				world.dispose();
				assets.dispose();
			} finally {
				Math.random = mathRandom;
			}
		}
	};
}
