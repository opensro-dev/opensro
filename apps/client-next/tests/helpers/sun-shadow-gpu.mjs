/*
===========================================================================

sun-shadow-gpu.mjs - where the sun cascade's shadow lands, through the production renderer

An off-axis pillar on flat ground under the pinned retail diagonal light
(the sun toward +x and up) casts a shadow whose ground footprint is exact
geometry: the pillar's base swept toward -x by its height. Proxy quads
drawn over that footprint, with the cascade off, mark its pixels for this
camera, so the test compares shadow and footprint without restating the
camera's projection. The fixture is serialized by Playwright unchanged.

===========================================================================
*/
/*
================
captureSunShadowPlacement

Returns one capture per scene: bare ground, the pillar, the pillar with
the cascade on, and the pillar over the inner and outer footprint proxies.
================
*/
export async function captureSunShadowPlacement() {
	const { createRenderer } = await import( "/src/engine/runtime/renderer/renderer.ts" );
	const { createPresentationRandom } = await import( "/src/engine/runtime/random/random.ts" );
	const { defaultVideoOptions } = await import( "/src/engine/foundation/rendering/video-options.ts" );
	const width = 192, height = 120;
	// The pillar sits off both cascade axes, so a mirrored cascade lookup
	// lands its shadow elsewhere on the ground.
	const PILLAR_X = 20, PILLAR_Z = 20, PILLAR_SIZE = 10, PILLAR_HEIGHT = 20;
	// The proxies shrink and grow the footprint by the PCF penumbra and bias.
	const INNER_MARGIN = 1.5, OUTER_MARGIN = 2.5, PROXY_LIFT = 0.05;
	const canvas = document.createElement( "canvas" );
	const output = document.createElement( "canvas" );
	output.width = width;
	output.height = height;
	const context = output.getContext( "2d", { willReadFrequently: true } );
	if ( !context ) throw Error( "No capture context" );
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
		skyDetail: false,
		smoothCamera: false,
		enchantSparks: false,
		sharedActorRoom: false,
		vHidesCompanions: false,
		dungeonCharacterPvs: false
	};
	const lit = shade => ({
		color: shade,
		unlit: false,
		objectLight: 1,
		alphaCutoff: 0,
		blend: false,
		doubleSided: true
	});
	/*
	================
	quad

	A flat, upward-facing rectangle at height y.
	================
	*/
	const quad = ( id, x0, z0, x1, z1, y, shade ) => ({
		id,
		center: [ (x0 + x1) / 2, y, (z0 + z1) / 2 ],
		radius: Math.hypot( x1 - x0, z1 - z0 ),
		material: lit( shade ),
		geometry: {
			world: true,
			positions: Float32Array.of( x0, y, z0, x1, y, z0, x1, y, z1, x0, y, z1 ),
			normals: Float32Array.of( 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0 ),
			uvs: Float32Array.of( 0, 0, 1, 0, 1, 1, 0, 1 ),
			indices: Uint32Array.of( 0, 1, 2, 0, 2, 3 ),
			instances: identity,
			transform: identity
		}
	});
	/*
	================
	pillar

	A closed box with flat outward normals.
	================
	*/
	const pillar = () => {
		const x0 = PILLAR_X, x1 = PILLAR_X + PILLAR_SIZE, z0 = PILLAR_Z, z1 = PILLAR_Z + PILLAR_SIZE;
		const y1 = PILLAR_HEIGHT;
		/** @type {{ corners: number[][], normal: number[] }[]} */
		const faces = [
			{ corners: [ [ x0, y1, z0 ], [ x1, y1, z0 ], [ x1, y1, z1 ], [ x0, y1, z1 ] ], normal: [ 0, 1, 0 ] },
			{ corners: [ [ x0, 0, z0 ], [ x1, 0, z0 ], [ x1, y1, z0 ], [ x0, y1, z0 ] ], normal: [ 0, 0, -1 ] },
			{ corners: [ [ x0, 0, z1 ], [ x1, 0, z1 ], [ x1, y1, z1 ], [ x0, y1, z1 ] ], normal: [ 0, 0, 1 ] },
			{ corners: [ [ x0, 0, z0 ], [ x0, 0, z1 ], [ x0, y1, z1 ], [ x0, y1, z0 ] ], normal: [ -1, 0, 0 ] },
			{ corners: [ [ x1, 0, z0 ], [ x1, 0, z1 ], [ x1, y1, z1 ], [ x1, y1, z0 ] ], normal: [ 1, 0, 0 ] }
		];
		/** @type {number[]} */
		const positions = [], normals = [], indices = [];
		for ( const { corners, normal } of faces ) {
			const base = positions.length / 3;
			for ( const corner of corners ) {
				positions.push( ...corner );
				normals.push( ...normal );
			}
			indices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
		}
		return {
			id: "pillar",
			center: [ x0 + PILLAR_SIZE / 2, y1 / 2, z0 + PILLAR_SIZE / 2 ],
			radius: PILLAR_HEIGHT,
			material: lit( [ .5, .55, .65, 1 ] ),
			geometry: {
				world: true,
				positions: new Float32Array( positions ),
				normals: new Float32Array( normals ),
				uvs: new Float32Array( positions.length / 3 * 2 ),
				indices: new Uint32Array( indices ),
				instances: identity,
				transform: identity
			}
		};
	};
	/*
	================
	footprint

	The pillar's ground shadow, grown by margin (a negative margin shrinks
	it). The light direction (1, 1, 0) sweeps the base toward -x by the
	pillar's height; base is its far +x edge, the pillar's own base for the
	outer proxy (the pillar hides it, and its foot gathers the penumbra) or
	the pillar's sunless face for the inner one.
	================
	*/
	const footprint = ( id, margin, base ) =>
		quad(
			id,
			PILLAR_X - PILLAR_HEIGHT - margin,
			PILLAR_Z - margin,
			base + margin,
			PILLAR_Z + PILLAR_SIZE + margin,
			PROXY_LIFT,
			[ .1, .9, .1, 1 ]
		);
	const scenes = [
		{ mode: "bare", groups: [], shadow: false },
		{ mode: "native", groups: [ pillar() ], shadow: false },
		{ mode: "shadow", groups: [ pillar() ], shadow: true },
		{ mode: "inner", groups: [ pillar(), footprint( "inner", -INNER_MARGIN, PILLAR_X ) ], shadow: false },
		{
			mode: "outer",
			groups: [ pillar(), footprint( "outer", OUTER_MARGIN, PILLAR_X + PILLAR_SIZE ) ],
			shadow: false
		}
	];
	try {
		const deadline = performance.now() + 15000;
		while ( renderer.phase() === "starting" && performance.now() < deadline ) {
			await new Promise( requestAnimationFrame );
		}
		if ( renderer.phase() !== "running" ) throw Error( renderer.error() ?? "GPU startup deadline" );
		renderer.setWorldCamera( { eye: [ 4, 34, -6 ], target: [ 10, 0, 23 ], fov: 1.1, near: 1, far: 500 } );
		renderer.videoOptions( defaultVideoOptions() );
		const rows = [];
		for ( const { mode, groups, shadow } of scenes ) {
			renderer.setWorld( {
				id: "sun-shadow-placement-" + mode,
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
				groups: [ quad( "ground", -120, -120, 120, 120, 0, [ .72, .62, .5, 1 ] ), ...groups ]
			} );
			renderer.experimentalVideo( { ...options, sunShadow: shadow } );
			for ( let frame = 0; frame < 5; frame++ ) await renderer.frame( { width, height }, .16 );
			if ( renderer.error() ) throw Error( `${mode}: ${renderer.error()}` );
			const image = await createImageBitmap( canvas );
			context.drawImage( image, 0, 0 );
			image.close();
			rows.push( { mode, rgba: [ ...context.getImageData( 0, 0, width, height ).data ] } );
		}
		return rows;
	} finally {
		renderer.dispose();
	}
}
