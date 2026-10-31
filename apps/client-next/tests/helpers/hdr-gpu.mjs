/*
===========================================================================

hdr-gpu.mjs - deterministic lighting captures through the production renderer

The fixture is serialized by Playwright unchanged for comparison across
reviewed trees. Device destruction exercises the real recovery lifecycle.

===========================================================================
*/
/*
================
captureLightingStages
================
*/
export async function captureLightingStages( { startup = false, recover = false, nativeOnly = false } = {} ) {
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
	const { defaultVideoOptions, changeVideo } = await import( "/src/engine/foundation/rendering/video-options.ts" );
	const width = 192, height = 120;
	const canvas = document.createElement( "canvas" );
	const output = document.createElement( "canvas" );
	output.width = width;
	output.height = height;
	const context = output.getContext( "2d", { willReadFrequently: true } );
	if ( !context ) throw Error( "No capture context" );
	const devices = [];
	const requestDevice = GPUAdapter.prototype.requestDevice;
	if ( recover ) {
		GPUAdapter.prototype.requestDevice = async function( descriptor ) {
			const device = await requestDevice.call( this, descriptor );
			devices.push( device );
			return device;
		};
	}
	const renderer = createRenderer( canvas, createPresentationRandom( 1 ) );
	const identity = Float32Array.of( 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1 );
	/** @type {import("../../src/engine/foundation/ui/experimental-options.ts").ExperimentalVideo} */
	const options = {
		renderScale: 100,
		phoneRenderCap: false,
		postProcessing: false,
		anisotropicFiltering: false,
		heightFog: false,
		dynamicSun: false,
		floatBloom: false,
		hdrToneMap: false,
		sunShadow: false,
		perPixelLighting: false,
		waterRefraction: false,
		softParticles: false,
		smoothCamera: false,
		enchantSparks: false,
		sharedActorRoom: false,
		vHidesCompanions: false,
		dungeonCharacterPvs: false
	};
	if ( startup ) renderer.experimentalVideo( { ...options, hdrToneMap: true, sunShadow: true } );
	// The dome: one apex, eight rim vertices with smooth outward normals,
	// so the saturated per-vertex colours and the per-pixel re-evaluation
	// disagree across its faces. The box sits sunward (+x) of the dome, so
	// the retail diagonal light casts it across the dome's ground.
	const SEGMENTS = 8;
	const positions = [ 0, 10, 0 ], normals = [ 0, 1, 0 ], indices = [];
	for ( let i = 0; i <= SEGMENTS; i++ ) {
		const angle = i / SEGMENTS * Math.PI * 2, x = Math.cos( angle ) * 18, z = Math.sin( angle ) * 18;
		positions.push( x, 0, z );
		const length = Math.hypot( x, 8, z );
		normals.push( x / length, 8 / length, z / length );
	}
	for ( let i = 0; i < SEGMENTS; i++ ) indices.push( 0, 1 + i, 2 + i );
	// A cube: 8 corners, 12 triangles, flat outward normals.
	const cube = [], cubeNormals = [], cubeIndices = [];
	const corners = [
		[ 0, 0, 0 ],
		[ 8, 0, 0 ],
		[ 8, 0, 8 ],
		[ 0, 0, 8 ],
		[ 0, 8, 0 ],
		[ 8, 8, 0 ],
		[ 8, 8, 8 ],
		[ 0, 8, 8 ]
	];
	const faces = [
		[ [ 0, 3, 2, 1 ], [ 0, -1, 0 ] ],
		[ [ [ 4 ], [ 5 ], [ 6 ], [ 7 ] ].flat(), [ 0, 1, 0 ] ],
		[ [ 0, 1, 5, 4 ], [ 0, 0, -1 ] ],
		[ [ [ 2 ], [ 3 ], [ 7 ], [ 6 ] ].flat(), [ 0, 0, 1 ] ],
		[ [ 1, 2, 6, 5 ], [ 1, 0, 0 ] ],
		[ [ [ 0 ], [ 4 ], [ 7 ], [ 3 ] ].flat(), [ -1, 0, 0 ] ]
	];
	for ( const [quad, normal] of faces ) {
		const base = cube.length / 3;
		for ( const corner of quad ) {
			cube.push( corners[corner][0] + 14, corners[corner][1], corners[corner][2] - 4 );
			cubeNormals.push( normal[0], normal[1], normal[2] );
		}
		cubeIndices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
	}
	const lit = shade => ({
		color: shade,
		unlit: false,
		objectLight: 1,
		alphaCutoff: 0,
		blend: false,
		doubleSided: true
	});
	try {
		const deadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < deadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
		renderer.setWorldCamera( { eye: [ 6, 26, -64 ], target: [ 4, 2, 0 ], fov: 1.1, near: 1, far: 500 } );
		renderer.setWorld( {
			id: "hdr-stages",
			originRegion: 257,
			warnings: [],
			environment: {
				startTimeOfDay: .5,
				ratePerSecond: 0,
				tracks: {
					color0xf0: [ { t: 0, r: 1, g: 1, b: 1 } ],
					color0x124: [ { t: 0, r: .55, g: .55, b: .55 } ]
				}
			},
			groups: [
				{
					id: "ground",
					center: [ 0, 0, 0 ],
					radius: 300,
					material: lit( [ .72, .62, .5, 1 ] ),
					geometry: {
						world: true,
						positions: Float32Array.of( -120, 0, -120, 120, 0, -120, 120, 0, 120, -120, 0, 120 ),
						normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
						uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
						indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
						instances: identity,
						transform: identity
					}
				},
				{
					id: "dome",
					center: [ 0, 5, 0 ],
					radius: 24,
					material: lit( [ .8, .45, .35, 1 ] ),
					geometry: {
						world: true,
						positions: new Float32Array( positions ),
						normals: new Float32Array( normals ),
						uvs: new Float32Array( positions.length / 3 * 2 ),
						indices: new Uint32Array( indices ),
						instances: identity,
						transform: identity
					}
				},
				{
					id: "box",
					center: [ 18, 4, 0 ],
					radius: 12,
					material: lit( [ .5, .55, .65, 1 ] ),
					geometry: {
						world: true,
						positions: new Float32Array( cube ),
						normals: new Float32Array( cubeNormals ),
						uvs: new Float32Array( cube.length / 3 * 2 ),
						indices: new Uint32Array( cubeIndices ),
						instances: identity,
						transform: identity
					}
				}
			]
		} );
		const rows = [];
		const modes = nativeOnly ? [ "off" ] : startup ?
			[ "hdrToneMap", "off" ] :
			[ "off", "hdrToneMap", "off", "sunShadow", "off", "perPixelLighting", "off", "combined", "off" ];
		for ( const mode of modes ) {
			renderer.videoOptions( changeVideo( defaultVideoOptions(), 11, mode === "combined" ? 1 : 0 ) );
			renderer.experimentalVideo( {
				...options,
				...(mode === "combined" ?
					{ hdrToneMap: true, sunShadow: true, perPixelLighting: true, floatBloom: true } :
					mode === "off" ?
					{} :
					{ [mode]: true })
			} );
			for ( let frame = 0; frame < 5; frame++ ) await renderer.frame( { width, height }, .16 );
			if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
			const image = await createImageBitmap( canvas );
			context.drawImage( image, 0, 0 );
			image.close();
			rows.push( { mode, rgba: [ ...context.getImageData( 0, 0, width, height ).data ] } );
			if ( recover && mode === "hdrToneMap" ) {
				const old = devices.at( -1 );
				old.destroy();
				await old.lost;
				const deadline = performance.now() + 15000;
				do {
					await renderer.frame( { width, height }, .16 );
					await new Promise( requestAnimationFrame );
				} while (
					(devices.at( -1 ) === old || renderer.phase() === "starting") && performance.now() < deadline
				);
				if ( devices.at( -1 ) === old ) throw Error( "Device replacement did not occur" );
				for ( let frame = 0; frame < 5; frame++ ) await renderer.frame( { width, height }, .16 );
				if ( renderer.error() ) throw Error( "Recovered HDR: " + renderer.error() );
				const image = await createImageBitmap( canvas );
				context.drawImage( image, 0, 0 );
				image.close();
				rows.push( { mode: "recovered", rgba: [ ...context.getImageData( 0, 0, width, height ).data ] } );
			}
		}
		return rows;
	} finally {
		renderer.dispose();
		if ( recover ) GPUAdapter.prototype.requestDevice = requestDevice;
	}
}
