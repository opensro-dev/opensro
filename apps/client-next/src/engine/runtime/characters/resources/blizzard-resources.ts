/*
===========================================================================
blizzard-resources.ts - owned frost, wind, snow and faceted ice for Blizzard.

Four bounded meshes share the normal character resource cache. Their clocks
are cosmetic; the server owns all five damage pulses and the final freeze.
===========================================================================
*/
import type { CharacterModel, CharacterChannel } from "@/engine/contracts/character";
import type { NativeTexture } from "@/engine/contracts/texture";
import { base64Bytes } from "@/engine/foundation/assets/base64";
import { penguinBlizzardAtlas } from "@/engine/foundation/assets/penguin-blizzard-atlas";
import { buildProceduralEffectModel, createIdentityMatrix } from "./effect-model";

/*
================
createBlizzardResource

Construction is lazy: unrelated effects never decode or allocate this atlas.
================
*/
export function createBlizzardResource( path: string ): { model: CharacterModel; images: NativeTexture[]; } | null {
	if (
		![
			"/assets/effects/penguin-frost-sigil",
			"/assets/effects/penguin-blizzard-wind",
			"/assets/effects/penguin-blizzard-snow",
			"/assets/effects/penguin-blizzard-ice"
		].includes( path )
	) return null;
	const ground = path.endsWith( "frost-sigil" ), wind = path.endsWith( "-wind" ), snow = path.endsWith( "-snow" );
	const positions: number[] = [],
		normals: number[] = [],
		uvs: number[] = [],
		indices: number[] = [],
		joints: number[] = [];
	const nodes: CharacterModel["nodes"][number][] = [], channels: CharacterChannel[] = [];
	const texture: NativeTexture = {
		kind: "native-texture",
		width: 512,
		height: 512,
		format: "bc3-rgba-unorm",
		levels: [ base64Bytes( penguinBlizzardAtlas.blocks ) ]
	};
	if ( ground ) {
		positions.push( 0, .004, 0 );
		normals.push( 0, 1, 0 );
		uvs.push( .75, .75 );
		for ( let i = 0; i <= 96; i++ ) {
			const a = i / 96 * Math.PI * 2, x = Math.cos( a ), z = Math.sin( a );
			positions.push( x, .004, z );
			normals.push( 0, 1, 0 );
			uvs.push( .75 + x * .24, .75 + z * .24 );
			if ( i < 96 ) indices.push( 0, i + 1, i + 2 );
		}
		const rim = positions.length / 3;
		for ( let i = 0; i <= 96; i++ ) {
			const angle = i / 96 * Math.PI * 2;
			for ( const radius of [ .985, 1 ] ) {
				positions.push( Math.cos( angle ) * radius, .012, Math.sin( angle ) * radius );
				normals.push( 0, 1, 0 );
				uvs.push( .75, .75 );
			}
			if ( i < 96 ) {
				const b = rim + i * 2;
				indices.push( b, b + 2, b + 1, b + 1, b + 2, b + 3 );
			}
		}
	} else if ( wind ) {
		for ( let ribbon = 0; ribbon < 6; ribbon++ ) {
			const base = positions.length / 3;
			for ( let i = 0; i <= 80; i++ ) {
				const t = i / 80, a = ribbon * Math.PI / 3 + t * Math.PI * 1.6, radius = .15 + .82 * t;
				for ( const side of [ -1, 1 ] ) {
					const height = .03 + Math.sin( t * Math.PI ) * (.12 + ribbon % 3 * .06) + side * .025;
					positions.push( Math.cos( a ) * radius, height, Math.sin( a ) * radius );
					normals.push( 0, 1, 0 );
					uvs.push( .5 + t * .5, (side + 1) * .25 );
				}
				if ( i < 80 ) {
					const b = base + i * 2;
					indices.push( b, b + 2, b + 1, b + 1, b + 2, b + 3 );
				}
			}
		}
		// Low textured gusts give the storm depth without an opaque ground square.
		for ( let i = 0; i < 8; i++ ) {
			const b = positions.length / 3, angle = i * Math.PI / 4;
			const x = Math.cos( angle ) * .45, z = Math.sin( angle ) * .45;
			for ( const [dx, dz] of [ [ -.4, -.25 ], [ .4, -.25 ], [ -.4, .25 ], [ .4, .25 ] ] ) {
				positions.push( x + dx!, .07 + (i % 3) * .025, z + dz! );
				normals.push( 0, 1, 0 );
				uvs.push( dx! > 0 ? .49 : .01, dz! > 0 ? .49 : .01 );
			}
			indices.push( b, b + 1, b + 2, b + 2, b + 1, b + 3 );
		}
	} else if ( snow ) {
		for ( let i = 0; i < 64; i++ ) {
			const size = i < 18 ? .035 : .012, base = positions.length / 3;
			for ( const plane of [ 0, 1, 2 ] ) {
				for ( const [x, y] of [ [ -1, -1 ], [ 1, -1 ], [ -1, 1 ], [ 1, 1 ] ] ) {
					positions.push(
						plane === 2 ? 0 : x! * size,
						plane === 1 ? 0 : y! * size,
						plane === 0 ? 0 : (plane === 1 ? y! : x!) * size
					);
					normals.push( 0, 1, 0 );
					uvs.push( (x! + 1) * .25, .5 + (y! + 1) * .25 );
					joints.push( i, 0, 0, 0 );
				}
			}
			for ( let b = base; b < positions.length / 3; b += 4 ) indices.push( b, b + 1, b + 2, b + 2, b + 1, b + 3 );
			nodes.push( {
				name: "snow-" + i,
				parent: -1,
				translation: [ 0, 0, 0 ],
				rotation: [ 0, 0, 0, 1 ],
				scale: [ 1, 1, 1 ]
			} );
			const times = Array.from( { length: 25 }, ( _, k ) => k / 24 ),
				reach = .10 + .85 * ((i * 17 % 64) / 63),
				angle = i * 2.399963;
			channels.push( {
				node: i,
				path: "translation",
				interpolation: "LINEAR",
				times: Float32Array.from( times ),
				values: Float32Array.from( times.flatMap( t => {
					const phase = (t + i / 64) % 1, a = angle + t * 1.4;
					return [ Math.cos( a ) * reach, .02 + .55 * (1 - phase), Math.sin( a ) * reach ];
				} ) )
			} );
			channels.push( {
				node: i,
				path: "scale",
				interpolation: "LINEAR",
				times: Float32Array.from( times ),
				values: Float32Array.from(
					times.flatMap( t => Array( 3 ).fill( Math.sin( ((t + i / 64) % 1) * Math.PI ) ) )
				)
			} );
		}
	} else {
		for ( let i = 0; i < 24; i++ ) {
			const cluster = Math.floor( i / 4 ), offset = i % 4, a = cluster * Math.PI / 3;
			const radius = .018 + offset * .004, height = .09 + (offset % 3) * .045;
			for ( let face = 0; face < 6; face++ ) {
				const b = positions.length / 3, a0 = face * Math.PI / 3, a1 = (face + 1) * Math.PI / 3;
				positions.push(
					Math.cos( a0 ) * radius,
					0,
					Math.sin( a0 ) * radius,
					Math.cos( a1 ) * radius,
					0,
					Math.sin( a1 ) * radius,
					Math.cos( a0 ) * radius * .65,
					height * .65,
					Math.sin( a0 ) * radius * .65,
					Math.cos( a1 ) * radius * .65,
					height * .65,
					Math.sin( a1 ) * radius * .65,
					0,
					height,
					0
				);
				for ( let k = 0; k < 5; k++ ) {
					normals.push( Math.cos( a0 ), .3, Math.sin( a0 ) );
					joints.push( i, 0, 0, 0 );
				}
				const u = face % 2 ? .8 : .25;
				uvs.push( u, .05, u, .05, u, .55, u, .55, u, 1 );
				indices.push( b, b + 1, b + 2, b + 1, b + 3, b + 2, b + 2, b + 3, b + 4 );
			}
			const reach = .78 + offset * .035;
			nodes.push( {
				name: "ice-" + i,
				parent: -1,
				translation: [ Math.cos( a ) * reach + offset * .025, 0, Math.sin( a ) * reach ],
				rotation: [ 0, Math.sin( a / 2 ), 0, Math.cos( a / 2 ) ],
				scale: [ 1, 1, 1 ]
			} );
			channels.push( {
				node: i,
				path: "scale",
				interpolation: "LINEAR",
				times: Float32Array.from( [ 0, .15, .5, 4.5, 5 ] ),
				values: Float32Array.from( [ 0, .15, 1, 1, .05 ].flatMap( v => [ v, v, v ] ) )
			} );
		}
	}
	const icePixels = new Uint8Array( 16 * 32 * 4 );
	for ( let y = 0; y < 32; y++ ) {
		for ( let x = 0; x < 16; x++ ) {
			const facet = x > 8 ? 1 : .52, tip = y / 31;
			icePixels.set( [
				Math.round( 220 + 35 * tip ),
				Math.round( (155 + 100 * tip) * facet ),
				Math.round( (55 + 185 * tip) * facet ),
				235
			], (y * 16 + x) * 4 );
		}
	}

	const resource = buildProceduralEffectModel(
		positions,
		normals,
		uvs,
		indices,
		!ground && !wind && !snow ?
			{ kind: "native-texture", width: 16, height: 32, format: "bgra8unorm", levels: [ icePixels ] } :
			texture
	);
	const primitive = resource.model.primitives[0]!;
	return {
		images: resource.images,
		model: {
			...resource.model,
			...(nodes.length ? { nodes, clips: [ { name: "effect", duration: snow ? 1 : 5, channels } ] } : {}),
			primitives: [ {
				...primitive,
				...(nodes.length ?
					{
						joints: nodes.map( ( _, i ) => i ),
						inverseBind: Float32Array.from( nodes.flatMap( () => [ ...createIdentityMatrix() ] ) )
					} :
					{}),
				geometry: {
					...primitive.geometry,
					...(nodes.length ? { joints: Uint32Array.from( joints ) } : {}),
					material: {
						...primitive.geometry.material!,
						fadeAlphaOnly: false,
						blendPair: { source: 5, destination: 6 }
					}
				}
			} ]
		}
	};
}
