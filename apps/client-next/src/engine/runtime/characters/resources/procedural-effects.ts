/*
===========================================================================
procedural-effects.ts - authored pet effect meshes and native texture resources.

Keep geometry construction separate from resource admission and cache lifetime.
===========================================================================
*/
import {
	PENGUIN_ICE_SHARD,
	PENGUIN_ICE_LANCE,
	PENGUIN_FROST_SIGIL
} from "@/engine/foundation/animation/penguin-effects";
import type { CharacterModel } from "@/engine/contracts/character";
import type { NativeTexture } from "@/engine/contracts/texture";

/*
================
createIdentityMatrix

Return a fresh 4x4 column-major identity matrix for procedural effect bindings.
================
*/
function createIdentityMatrix(): Float32Array {
	const out = new Float32Array( 16 );
	out[0] =
		out[5] =
		out[10] =
		out[15] =
			1;
	return out;
}

/*
================
buildProceduralEffectModel

Wrap vertex streams and a procedural BGRA native texture into an admitted CharacterModel.
================
*/
function buildProceduralEffectModel(
	positions: number[],
	normals: number[],
	uvs: number[],
	indices: number[],
	texture: NativeTexture
): { model: CharacterModel; images: NativeTexture[]; } {
	const vertexCount = positions.length / 3;
	const weights = new Float32Array( vertexCount * 4 );
	for ( let i = 0; i < vertexCount; i++ ) {
		weights[i * 4] = 1;
	}
	return {
		model: {
			nodes: [
				{
					name: "root",
					parent: -1,
					translation: [ 0, 0, 0 ],
					rotation: [ 0, 0, 0, 1 ],
					scale: [ 1, 1, 1 ]
				}
			],
			clips: [ { name: "effect", duration: 1, channels: [] } ],
			images: [ { width: texture.width, height: texture.height } ],
			primitives: [
				{
					name: "effect",
					node: 0,
					joints: [ 0 ],
					inverseBind: createIdentityMatrix(),
					image: 0,
					geometry: {
						positions: Float32Array.from( positions ),
						normals: Float32Array.from( normals ),
						uvs: Float32Array.from( uvs ),
						joints: new Uint32Array( vertexCount * 4 ),
						weights,
						indices: Uint32Array.from( indices ),
						transform: createIdentityMatrix(),
						material: {
							color: [ 1, 1, 1, 1 ],
							alphaCutoff: 0,
							blend: true,
							blendPair: { source: 5, destination: 2 },
							depthWrite: false,
							textureAlpha: true,
							fadeAlphaOnly: true,
							doubleSided: true,
							unlit: true,
							fogDisabled: true
						}
					}
				}
			]
		},
		images: [ texture ]
	};
}

/*
================
createChallengingRoarAuraResource

Build the 3D golden-crimson primal aura shell and interlocking vortex rings that
brighten over the 1-second Challenging Roar buildup around the bear.
================
*/
function createChallengingRoarAuraResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const size = 128;
	const pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const v = (y + 0.5) / size;
		const envV = Math.pow( Math.sin( v * Math.PI ), 1.35 );
		const coreV = Math.pow( Math.sin( v * Math.PI ), 2.6 );
		for ( let x = 0; x < size; x++ ) {
			const u = (x + 0.5) / size;
			const angle = u * Math.PI * 2;
			const f1 = 0.5 + 0.5 * Math.sin( angle * 8 + v * 10 );
			const f2 = 0.5 + 0.5 * Math.cos( angle * 14 - v * 16 );
			const f3 = 0.5 + 0.5 * Math.sin( angle * 22 + v * 6 );
			const energy = envV * (0.38 + 0.34 * (f1 * f1) + 0.18 * (f2 * f2 * f2) + 0.10 * (f3 * f3));
			const r = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.50 + coreV * 0.42 ) ) );
			const g = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( energy, 1.32 ) * 1.08 + Math.pow( coreV, 1.8 ) * 0.58 ) )
			);
			const b = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( energy, 2.35 ) * 0.65 + Math.pow( coreV, 2.9 ) * 0.72 ) )
			);
			const a = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.18 ) ) );
			const idx = (y * size + x) * 4;
			pixels[idx] = b;
			pixels[idx + 1] = g;
			pixels[idx + 2] = r;
			pixels[idx + 3] = a;
		}
	}
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const segs = 32, rings = 8;
	// 1. Ovoid primal energy cocoon around the bear's torso/head.
	for ( let r = 0; r <= rings; r++ ) {
		const v = r / rings;
		const phi = (v - 0.5) * Math.PI * 0.88;
		const cp = Math.cos( phi ), sp = Math.sin( phi );
		for ( let s = 0; s <= segs; s++ ) {
			const u = s / segs, theta = u * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * cp * 10.5, 10.5 + sp * 11.5, st * cp * 10.5 );
			normals.push( ct * cp, sp, st * cp );
			uvs.push( u, v );
		}
	}
	for ( let r = 0; r < rings; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 2. Rising 8-pointed flame pillar crown around the bear.
	const crownBase = positions.length / 3;
	for ( let r = 0; r <= 4; r++ ) {
		const v = r / 4;
		const y = 0.8 + v * 22.0;
		const baseR = 11.5 + v * 3.8;
		for ( let s = 0; s <= segs; s++ ) {
			const u = s / segs, theta = u * Math.PI * 2;
			const star = 1 + 0.14 * Math.cos( theta * 8 + v * 1.8 );
			const rad = baseR * star;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.15, st );
			uvs.push( u, v );
		}
	}
	for ( let r = 0; r < 4; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = crownBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 3. Two interlocking tilted vortex bands precessing around the bear.
	for ( const tilt of [ 0.48, -0.48 ] ) {
		const bandBase = positions.length / 3;
		const cs = Math.cos( tilt ), sn = Math.sin( tilt );
		for ( let r = 0; r <= 2; r++ ) {
			const v = r / 2;
			const localY = (v - 0.5) * 4.6;
			const rad = 14.2 - Math.abs( v - 0.5 ) * 1.4;
			for ( let s = 0; s <= segs; s++ ) {
				const u = s / segs, theta = u * Math.PI * 2;
				const lx = Math.cos( theta ) * rad, lz = Math.sin( theta ) * rad;
				const wy = 10.0 + localY * cs - lz * sn;
				const wz = localY * sn + lz * cs;
				positions.push( lx, wy, wz );
				normals.push( Math.cos( theta ), cs, Math.sin( theta ) * cs );
				uvs.push( u, v );
			}
		}
		for ( let r = 0; r < 2; r++ ) {
			for ( let s = 0; s < segs; s++ ) {
				const a = bandBase + r * (segs + 1) + s, b = a + segs + 1;
				indices.push( a, b, a + 1, a + 1, b, b + 1 );
			}
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createChallengingRoarShockwaveResource

Build the unit-radius (1.0 world unit) 3D shockwave ring, curved supersonic
blast wall and secondary wake ring. Scaling an actor of this model to 250
produces an exact 25.0 m (250 world units) radial shockwave.
================
*/
function createChallengingRoarShockwaveResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const size = 256, half = size / 2;
	const pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const dy = (y + 0.5) / half - 1;
		for ( let x = 0; x < size; x++ ) {
			const dx = (x + 0.5) / half - 1;
			const r = Math.hypot( dx, dy );
			if ( r <= 0.24 || r >= 0.995 ) continue;
			const theta = Math.atan2( dy, dx );
			const outerFeather = r > 0.93 ? Math.max( 0, (0.995 - r) / 0.065 ) : 1;
			const crest = Math.exp( -Math.pow( (r - 0.865) / 0.042, 2 ) );
			const corona = Math.exp( -Math.pow( (r - 0.835) / 0.098, 2 ) );
			const ripple1 = Math.exp( -Math.pow( (r - 0.725) / 0.028, 2 ) ) * 0.74;
			const ripple2 = Math.exp( -Math.pow( (r - 0.610) / 0.024, 2 ) ) * 0.52;
			const ripple3 = Math.exp( -Math.pow( (r - 0.495) / 0.020, 2 ) ) * 0.34;
			const spokeEnv = r > 0.34 && r < 0.92 ?
				Math.sin( ((r - 0.34) / 0.58) * Math.PI ) :
				0;
			const spokes = (Math.pow( 0.5 + 0.5 * Math.cos( theta * 36 ), 6 ) * 0.68 +
				Math.pow( 0.5 + 0.5 * Math.sin( theta * 64 + r * 10 ), 8 ) * 0.56) *
				spokeEnv;
			const total = Math.min(
				1,
				(crest * 1.18 + corona * 0.66 + ripple1 + ripple2 + ripple3 + spokes * 0.62) * outerFeather
			);
			const whiteHot = Math.min( 1, (Math.pow( crest, 1.45 ) + spokes * crest * 0.65) * outerFeather );
			const red = Math.min( 255, Math.round( 255 * Math.min( 1, total * 1.55 ) ) );
			const green = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( total, 1.32 ) * 1.06 + whiteHot * 0.48 ) )
			);
			const blue = Math.min(
				255,
				Math.round( 255 * Math.min( 1, Math.pow( total, 2.55 ) * 0.55 + whiteHot * 0.72 ) )
			);
			const alpha = Math.min( 255, Math.round( 255 * total ) );
			const idx = (y * size + x) * 4;
			pixels[idx] = blue;
			pixels[idx + 1] = green;
			pixels[idx + 2] = red;
			pixels[idx + 3] = alpha;
		}
	}
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const segs = 64;
	// 1. Ground-hugging radial shockwave annulus (unit outer radius = 1.0).
	const discRadii = [ 0.26, 0.46, 0.64, 0.78, 0.88, 0.95, 1.0 ];
	const discHeights = [ 0.008, 0.014, 0.020, 0.026, 0.032, 0.022, 0.010 ];
	for ( let r = 0; r < discRadii.length; r++ ) {
		const rad = discRadii[r]!, y = discHeights[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( 0, 1, 0 );
			uvs.push( 0.5 + 0.5 * rad * ct, 0.5 + 0.5 * rad * st );
		}
	}
	for ( let r = 0; r < discRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 2. 3D curved supersonic blast-wave crest wall near the leading edge.
	const wallBase = positions.length / 3;
	const wallRadii = [ 0.78, 0.865, 0.935, 0.90, 0.82 ];
	const wallHeights = [ 0.012, 0.045, 0.095, 0.145, 0.175 ];
	const wallUvRadii = [ 0.72, 0.83, 0.865, 0.90, 0.96 ];
	for ( let r = 0; r < wallRadii.length; r++ ) {
		const rad = wallRadii[r]!, y = wallHeights[r]!, uvRad = wallUvRadii[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.35, st );
			uvs.push( 0.5 + 0.5 * uvRad * ct, 0.5 + 0.5 * uvRad * st );
		}
	}
	for ( let r = 0; r < wallRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = wallBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	// 3. Inner angled sonic compression wake ring.
	const wakeBase = positions.length / 3;
	const wakeRadii = [ 0.56, 0.65, 0.72, 0.68 ];
	const wakeHeights = [ 0.010, 0.038, 0.075, 0.105 ];
	const wakeUvRadii = [ 0.52, 0.62, 0.725, 0.95 ];
	for ( let r = 0; r < wakeRadii.length; r++ ) {
		const rad = wakeRadii[r]!, y = wakeHeights[r]!, uvRad = wakeUvRadii[r]!;
		for ( let s = 0; s <= segs; s++ ) {
			const theta = (s / segs) * Math.PI * 2;
			const ct = Math.cos( theta ), st = Math.sin( theta );
			positions.push( ct * rad, y, st * rad );
			normals.push( ct, 0.45, st );
			uvs.push( 0.5 + 0.5 * uvRad * ct, 0.5 + 0.5 * uvRad * st );
		}
	}
	for ( let r = 0; r < wakeRadii.length - 1; r++ ) {
		for ( let s = 0; s < segs; s++ ) {
			const a = wakeBase + r * (segs + 1) + s, b = a + segs + 1;
			indices.push( a, b, a + 1, a + 1, b, b + 1 );
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createIronHideShieldResource

Port-only crystalline-cyan ward: a translucent barrier dome with a hexagonal
energy lattice, glowing meridian/latitude ribs and interlocking equatorial rings.
================
*/
function createIronHideShieldResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	const columns = 32, rows = 18;
	for ( let y = 0; y <= rows; y++ ) {
		const v = y / rows;
		const theta = v * Math.PI * 0.88;
		const sinT = Math.sin( theta ), cosT = Math.cos( theta );
		for ( let x = 0; x <= columns; x++ ) {
			const u = x / columns;
			const phi = u * Math.PI * 2;
			const nx = sinT * Math.cos( phi ), ny = cosT, nz = sinT * Math.sin( phi );
			positions.push( nx, ny * 0.95 + 0.95, nz );
			normals.push( nx, ny, nz );
			uvs.push( u, v );
			if ( x < columns && y < rows ) {
				const i = y * (columns + 1) + x;
				indices.push( i, i + 1, i + columns + 1, i + 1, i + columns + 2, i + columns + 1 );
			}
		}
	}
	const ringConfigs = [
		{ tiltX: 0.24, tiltZ: -0.18, radius: 1.08, halfH: 0.12, centerY: 0.95 },
		{ tiltX: -0.22, tiltZ: 0.24, radius: 1.05, halfH: 0.10, centerY: 0.95 },
		{ tiltX: 0, tiltZ: 0, radius: 1.04, halfH: 0.08, centerY: 0.10 }
	];
	for ( const cfg of ringConfigs ) {
		const baseVertex = positions.length / 3;
		const cX = Math.cos( cfg.tiltX ), sX = Math.sin( cfg.tiltX );
		const cZ = Math.cos( cfg.tiltZ ), sZ = Math.sin( cfg.tiltZ );
		for ( let r = 0; r <= 2; r++ ) {
			const v = 0.35 + (r / 2) * 0.30;
			const ly = (r / 2 - 0.5) * 2 * cfg.halfH;
			for ( let s = 0; s <= columns; s++ ) {
				const u = s / columns;
				const phi = u * Math.PI * 2;
				const lx = Math.cos( phi ) * cfg.radius, lz = Math.sin( phi ) * cfg.radius;
				const y1 = ly * cX - lz * sX, z1 = ly * sX + lz * cX;
				const x2 = lx * cZ - y1 * sZ, y2 = lx * sZ + y1 * cZ + cfg.centerY;
				positions.push( x2, y2, z1 );
				normals.push( Math.cos( phi ), 0, Math.sin( phi ) );
				uvs.push( u, v );
			}
		}
		for ( let r = 0; r < 2; r++ ) {
			for ( let s = 0; s < columns; s++ ) {
				const i0 = baseVertex + r * (columns + 1) + s, i1 = i0 + 1;
				const i2 = i0 + (columns + 1), i3 = i2 + 1;
				indices.push( i0, i2, i1, i1, i2, i3 );
			}
		}
	}
	const size = 128, pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		const v = (y + 0.5) / size;
		const eqBand = Math.exp( -Math.pow( (v - 0.50) / 0.09, 2 ) );
		const baseBand = Math.exp( -Math.pow( (v - 0.92) / 0.045, 2 ) );
		const crownBand = Math.exp( -Math.pow( (v - 0.14) / 0.06, 2 ) );
		const latRib = Math.pow( Math.max( 0, Math.cos( v * Math.PI * 10 ) ), 16 );
		const shellFade = Math.sin( v * Math.PI );
		for ( let x = 0; x < size; x++ ) {
			const u = (x + 0.5) / size;
			const lonRib = Math.pow( Math.max( 0, Math.cos( u * Math.PI * 14 ) ), 18 );
			const hexA = Math.pow( Math.max( 0, Math.cos( (u * 10 + v * 6) * Math.PI * 2 ) ), 16 );
			const hexB = Math.pow( Math.max( 0, Math.cos( (u * 10 - v * 6) * Math.PI * 2 ) ), 16 );
			const lattice = Math.min( 1, Math.max( latRib, lonRib * 0.85, (hexA + hexB) * 0.65 ) );
			const energy = Math.min(
				1,
				0.04 * shellFade + 0.58 * lattice * shellFade + 0.34 * eqBand + 0.52 * baseBand + 0.24 * crownBand
			);
			const idx = (y * size + x) * 4;
			pixels[idx] = Math.min( 255, Math.round( 255 * Math.min( 1, energy * 1.35 ) ) );
			pixels[idx + 1] = Math.min( 255, Math.round( 245 * Math.min( 1, Math.pow( energy, 1.15 ) * 1.12 ) ) );
			pixels[idx + 2] = Math.min( 255, Math.round( 180 * Math.min( 1, Math.pow( energy, 1.75 ) * 0.85 ) ) );
			pixels[idx + 3] = Math.min( 255, Math.round( 230 * Math.pow( energy, 1.12 ) ) );
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createPetCounterRippleResource

A soft, unit-radius ground annulus without the roar's walls or bright spokes.
================
*/
function createPetCounterRippleResource( wind = false ): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	for ( let i = 0; i <= 64; i++ ) {
		const angle = i / 64 * Math.PI * (wind ? 3 : 2);
		for ( const edge of [ 0, 1 ] ) {
			const radius = wind ? .2 + .7 * i / 64 + edge * .025 : .96 + edge * .04;
			positions.push( Math.cos( angle ) * radius, wind ? .02 + .18 * i / 64 : .025, Math.sin( angle ) * radius );
			normals.push( 0, 1, 0 );
			uvs.push( wind ? i / 64 : .5, edge );
		}
		if ( i < 64 ) {
			const a = i * 2;
			indices.push( a, a + 2, a + 1, a + 1, a + 2, a + 3 );
		}
	}
	const width = wind ? 64 : 1;
	const pixels = new Uint8Array( width * 16 * 4 );
	for ( let y = 0; y < 16; y++ ) {
		for ( let x = 0; x < width; x++ ) {
			const tipFade = wind ? Math.sin( x / (width - 1) * Math.PI ) : 1;
			pixels.set(
				[ 220, 220, 220, Math.round( Math.sin( y / 15 * Math.PI ) * tipFade * 255 ) ],
				(y * width + x) * 4
			);
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width,
		height: 16,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createPenguinIceResource

An original faceted crystal and a snowflake seal; no retail spell program.
================
*/
function createPenguinIceResource(
	sigil: boolean,
	lance = false
): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	if ( sigil ) {
		// Geometry bounds the frost ring; additive blending must never expose a rectangular card.
		for ( let i = 0; i < 96; i++ ) {
			const base = positions.length / 3;
			for ( const angle of [ i * Math.PI / 48, (i + 1) * Math.PI / 48 ] ) {
				for ( const radius of [ .88, .94 ] ) {
					const x = Math.cos( angle ) * radius, z = Math.sin( angle ) * radius;
					positions.push( x, 0, z );
					normals.push( 0, 1, 0 );
					uvs.push( (x + 1) / 2, (z + 1) / 2 );
				}
			}
			indices.push( base, base + 2, base + 1, base + 1, base + 2, base + 3 );
		}
	} else {
		for ( let i = 0; i < 6; i++ ) {
			const a = i * Math.PI / 3, b = (i + 1) * Math.PI / 3;
			for ( const tip of lance ? [ 4, -1.4 ] : [ 1.2, -.7 ] ) {
				const base = positions.length / 3;
				positions.push(
					0,
					0,
					tip,
					Math.cos( a ) * .3,
					Math.sin( a ) * .3,
					0,
					Math.cos( b ) * .3,
					Math.sin( b ) * .3,
					0
				);
				for ( let j = 0; j < 3; j++ ) {
					normals.push( Math.cos( (a + b) / 2 ), Math.sin( (a + b) / 2 ), tip > 0 ? .3 : -.3 );
				}
				uvs.push( .5, 0, 0, 1, 1, 1 );
				indices.push( base, base + 1, base + 2 );
			}
		}
	}
	const size = sigil ? 128 : 16, pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		for ( let x = 0; x < size; x++ ) {
			const u = (x + .5) / size, v = (y + .5) / size;
			const dx = u * 2 - 1, dz = v * 2 - 1, radius = Math.hypot( dx, dz ), angle = Math.atan2( dz, dx );
			const arm = Math.abs( Math.sin( angle * 3 ) ) * radius;
			const rings = Math.exp( -Math.pow( (radius - .91) * 90, 2 ) ) +
				.45 * Math.exp( -Math.pow( (radius - .77) * 90, 2 ) );
			const snow = Math.exp( -Math.pow( arm * 90, 2 ) ) * Math.max( 0, 1 - radius / .7 );
			const branches = Math.pow( Math.max( 0, Math.cos( angle * 6 + radius * 32 ) ), 16 ) *
				Math.exp( -Math.pow( (radius - .46) * 9, 2 ) );
			const glow = sigil ?
				Math.min( 1, rings + snow + branches * .7 ) * (radius < 1 ? 1 : 0) :
				.55 + .45 * Math.pow( 1 - Math.abs( u - .5 ) * 2, 3 );
			pixels.set(
				[
					Math.round( 230 * glow ),
					Math.round( 165 * glow ),
					Math.round( 85 * glow ),
					Math.round( glow * 255 )
				],
				(y * size + x) * 4
			);
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createCrowFeatherResource

An owned curved feather silhouette; tapered vanes and a bright central shaft.
================
*/
function createCrowFeatherResource(): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	for ( let i = 0; i <= 16; i++ ) {
		const t = i / 16, width = Math.sin( Math.PI * t ) * .6;
		for ( const edge of [ -1, 1 ] ) {
			positions.push( edge * width, Math.sin( t * Math.PI ) * .15, (t - .5) * 3 );
			normals.push( 0, 1, 0 );
			uvs.push( edge < 0 ? 0 : 1, t );
		}
		if ( i < 16 ) {
			const v = i * 2;
			indices.push( v, v + 2, v + 1, v + 1, v + 2, v + 3 );
		}
	}
	const size = 64, pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		for ( let x = 0; x < size; x++ ) {
			const u = x / (size - 1), t = y / (size - 1), shaft = Math.exp( -Math.pow( (u - .5) * 20, 2 ) );
			const vane = Math.abs( u - .5 );
			const barbs = .48 + .22 * Math.cos( t * 135 + vane * 46 ) + .3 * shaft;
			const glint = Math.pow( Math.max( 0, Math.cos( t * 71 - vane * 25 ) ), 12 ) * .15;
			const alpha = Math.sin( u * Math.PI ) * Math.sin( t * Math.PI );
			const light = Math.min( 1, barbs + glint );
			pixels.set(
				[ light * 235, light * 245, light * 250, alpha * 255 ],
				(y * size + x) * 4
			);
		}
	}
	return buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
}

/*
================
createCrowRecoveryResource

Crossed soft sprites keep recovery wisps visible from every camera angle.
================
*/
function createCrowRecoveryResource( mana: boolean ): { model: CharacterModel; images: NativeTexture[]; } {
	const positions: number[] = [], normals: number[] = [], uvs: number[] = [], indices: number[] = [];
	for ( let plane = 0; plane < 3; plane++ ) {
		const angle = plane * Math.PI / 3, base = positions.length / 3;
		for ( const [x, y, u, v] of [ [ -1, -1, 0, 1 ], [ 1, -1, 1, 1 ], [ 1, 1, 1, 0 ], [ -1, 1, 0, 0 ] ] as const ) {
			const width = mana ? .3 : .65;
			positions.push( Math.cos( angle ) * x * width, y * (mana ? 1.8 : .9), Math.sin( angle ) * x * width );
			normals.push( -Math.sin( angle ), 0, Math.cos( angle ) );
			uvs.push( u, v );
		}
		indices.push( base, base + 1, base + 2, base, base + 2, base + 3 );
	}
	const size = 64, pixels = new Uint8Array( size * size * 4 );
	for ( let y = 0; y < size; y++ ) {
		for ( let x = 0; x < size; x++ ) {
			const u = x / (size - 1) * 2 - 1, v = y / (size - 1) * 2 - 1;
			const edge = Math.max( 0, (1 - u * u) * (1 - v * v) );
			const glow = Math.exp( -u * u * 5 - v * v * 3 ) * edge * edge;
			const core = Math.exp( -u * u * 30 - v * v * 18 );
			const rgb: readonly [number, number, number] = mana ?
				[ 40 + core * 65, 115 + core * 65, 245 ] :
				[ 235, 35 + core * 75, 65 + core * 45 ];
			pixels.set( [ rgb[2], rgb[1], rgb[0], Math.round( glow * 210 ) ], (y * size + x) * 4 );
		}
	}
	const resource = buildProceduralEffectModel( positions, normals, uvs, indices, {
		kind: "native-texture",
		width: size,
		height: size,
		format: "bgra8unorm",
		levels: [ pixels ]
	} );
	// Straight alpha keeps red/blue legible over bright terrain instead of washing them to gold.
	return {
		...resource,
		model: {
			...resource.model,
			primitives: resource.model.primitives.map( p => ({
				...p,
				geometry: {
					...p.geometry,
					material: {
						...p.geometry.material!,
						fadeAlphaOnly: false,
						blendPair: { source: 5, destination: 6 } as const
					}
				}
			}) )
		}
	};
}

/*
================
createProceduralEffect

Explicit calls keep effect execution visible to the capability contract.
================
*/
export function createProceduralEffect( path: string ) {
	switch ( path ) {
		case "/assets/effects/crow-heal-wisp":
			return createCrowRecoveryResource( false );
		case "/assets/effects/crow-mana-wisp":
			return createCrowRecoveryResource( true );
		case "/assets/effects/crow-feather":
			return createCrowFeatherResource();
		case PENGUIN_ICE_SHARD:
			return createPenguinIceResource( false );
		case PENGUIN_ICE_LANCE:
			return createPenguinIceResource( false, true );
		case PENGUIN_FROST_SIGIL:
			return createPenguinIceResource( true );
		case "/assets/effects/crow-wind-burst":
			return createPetCounterRippleResource( true );
		case "/assets/effects/pet-counter-ripple":
			return createPetCounterRippleResource();
		case "/assets/effects/iron-hide-shield":
			return createIronHideShieldResource();
		case "/assets/effects/challenging-roar-aura":
			return createChallengingRoarAuraResource();
		case "/assets/effects/challenging-roar-shockwave":
			return createChallengingRoarShockwaveResource();
		default:
			return null;
	}
}
