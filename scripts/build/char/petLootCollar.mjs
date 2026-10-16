/*
===========================================================================

petLootCollar.mjs - owned caravan collar ground prop for pet equipment

Port-only, not native: owner-selected concept 2, 2026-10-15. Muted teal,
antique brass, two short red tassels and a turquoise sun medallion. Matches
the retail drop props' diffuse/ambient lighting. Owner requested 80% larger
and the native chest glow on 2026-10-15. Geometry is the editable source asset;
the normal avatar exporter supplies the runtime's native material contract.

===========================================================================
*/
import { avatarToGlb } from "./exportGlb.mjs";
import { fileURLToPath } from "node:url";

const SEGMENTS = 24;
const COLLAR_SCALE = 1.8;
// Original owned atlas: leather/brass/turquoise above lining/trim/red silk.
const MATERIAL_TILES = {
	leather: [ 0, 0 ],
	inside: [ 0, 1 ],
	brass: [ 1, 0 ],
	edge: [ 1, 1 ],
	stone: [ 2, 0 ],
	red: [ 2, 1 ]
};
const ATLAS_INSET = 0.006;

/*
================
createPetLootCollar

Static prop: original 4.26-unit width enlarged 80%, still grounded.
Retail accessory boxes span about 2.9 units and armor chests about 4.7 units. Flat normals
and modest facets retain the low-poly readability of the surrounding loot.
================
*/
export function createPetLootCollar() {
	const meshes = new Map();
	/*
	================
	face
	================
	*/
	function face( material, vertices, textureCoordinates ) {
		if ( !meshes.has( material ) ) {
			meshes.set( material, {
				materialName: material,
				rigid: true,
				positions: [],
				normals: [],
				uvs: [],
				indices: []
			} );
		}
		const mesh = meshes.get( material ), base = mesh.positions.length / 3;
		const a = vertices[0], b = vertices[1], c = vertices[2];
		const u = b.map( ( v, i ) => v - a[i] ), v = c.map( ( n, i ) => n - a[i] );
		const normal = [ u[1] * v[2] - u[2] * v[1], u[2] * v[0] - u[0] * v[2], u[0] * v[1] - u[1] * v[0] ];
		const length = Math.hypot( ...normal );
		if ( length === 0 ) throw Error( "Degenerate collar face" );
		for ( const [i, point] of vertices.entries() ) {
			mesh.positions.push( ...point );
			mesh.normals.push( ...normal.map( n => n / length ) );
			const uv = textureCoordinates?.[i] ?? [ i === 1 || i === 2 ? 1 : 0, i >= 2 ? 1 : 0 ];
			const tile = MATERIAL_TILES[material];
			mesh.uvs.push(
				(tile[0] + ATLAS_INSET + uv[0] * (1 - 2 * ATLAS_INSET)) / 3,
				(tile[1] + ATLAS_INSET + uv[1] * (1 - 2 * ATLAS_INSET)) / 2
			);
		}
		for ( let i = 1; i < vertices.length - 1; i++ ) mesh.indices.push( base, base + i, base + i + 1 );
	}
	/*
	================
	band

	An elliptical band with a rectangular cross-section, including its inside.
	================
	*/
	function band( material, rx, rz, thickness, low, high ) {
		for ( let i = 0; i < SEGMENTS; i++ ) {
			const angles = [ i, i + 1 ].map( n => n * Math.PI * 2 / SEGMENTS );
			const at = (
				j,
				inner,
				y
			) => [
				(rx - inner * thickness) * Math.cos( angles[j] ),
				y,
				(rz - inner * thickness) * Math.sin( angles[j] )
			];
			face( material, [ at( 0, 0, high ), at( 1, 0, high ), at( 1, 0, low ), at( 0, 0, low ) ] );
			face( material === "leather" ? "inside" : material, [
				at( 0, 1, high ),
				at( 1, 1, high ),
				at( 1, 1, low ),
				at( 0, 1, low )
			] );
			face( material, [ at( 0, 1, high ), at( 1, 1, high ), at( 1, 0, high ), at( 0, 0, high ) ] );
			face( material, [ at( 0, 0, low ), at( 1, 0, low ), at( 1, 1, low ), at( 0, 1, low ) ] );
		}
	}
	/*
	================
	box
	================
	*/
	function box( material, center, size, yaw = 0 ) {
		const corners = [ [ -1, -1, -1 ], [ 1, -1, -1 ], [ 1, 1, -1 ], [ -1, 1, -1 ], [ -1, -1, 1 ], [ 1, -1, 1 ], [
			1,
			1,
			1
		], [ -1, 1, 1 ] ].map( p => {
			const x = p[0] * size[0] / 2, z = p[2] * size[2] / 2;
			return [
				center[0] + x * Math.cos( yaw ) + z * Math.sin( yaw ),
				center[1] + p[1] * size[1] / 2,
				center[2] - x * Math.sin( yaw ) + z * Math.cos( yaw )
			];
		} );
		for (
			const indices of [ [ 0, 3, 2, 1 ], [ 4, 5, 6, 7 ], [ 0, 4, 7, 3 ], [ 1, 2, 6, 5 ], [ 3, 7, 6, 2 ], [
				0,
				1,
				5,
				4
			] ]
		) {
			face( material, indices.map( i => corners[i] ) );
		}
	}
	/*
	================
	medallion

	A shallow faceted disc facing outward, with a smaller raised stone.
	================
	*/
	function medallion( material, x, y, z, radius, depth, sides = 12 ) {
		const front = [ x, y, z - depth ];
		for ( let i = 0; i < sides; i++ ) {
			const a = i * Math.PI * 2 / sides, b = (i + 1) * Math.PI * 2 / sides;
			const p = [ x + Math.cos( a ) * radius, y + Math.sin( a ) * radius, z ];
			const q = [ x + Math.cos( b ) * radius, y + Math.sin( b ) * radius, z ];
			// One continuous disc, rather than a full texture repeated per wedge.
			const uv = angle => [ 0.5 + Math.cos( angle ) * 0.5, 0.5 - Math.sin( angle ) * 0.5 ];
			face( material, [ front, q, p ], [ [ 0.5, 0.5 ], uv( b ), uv( a ) ] );
			face( material, [ [ x, y, z + 0.06 ], p, q ], [ [ 0.5, 0.5 ], uv( a ), uv( b ) ] );
		}
	}

	band( "leather", 2.02, 1.40, 0.20, 0.16, 1.12 );
	band( "edge", 2.10, 1.46, 0.29, 0.13, 0.25 );
	band( "brass", 2.10, 1.46, 0.29, 1.03, 1.16 );
	for ( const angle of [ 0.25, 0.95, 2.19, 2.89, 3.65, 5.77 ] ) {
		const x = 2.04 * Math.cos( angle ), z = 1.43 * Math.sin( angle );
		box( "brass", [ x, 0.65, z ], [ 0.60, 0.65, 0.12 ], Math.PI / 2 - angle );
		box( "stone", [ x * 1.04, 0.65, z * 1.04 ], [ 0.20, 0.28, 0.10 ], Math.PI / 2 - angle );
	}
	// Back buckle: a hollow frame and tongue rather than an oversized solid block.
	for ( const x of [ -0.36, 0.36 ] ) box( "edge", [ x, 0.70, 1.49 ], [ 0.10, 0.80, 0.14 ] );
	for ( const y of [ 0.33, 1.07 ] ) box( "edge", [ 0, y, 1.49 ], [ 0.80, 0.10, 0.14 ] );
	box( "brass", [ 0, 0.70, 1.52 ], [ 0.08, 0.67, 0.12 ] );
	box( "edge", [ 0, 0.90, -1.49 ], [ 0.16, 0.38, 0.15 ] );
	medallion( "edge", 0, 0.61, -1.65, 0.53, 0.10 );
	medallion( "brass", 0, 0.61, -1.77, 0.43, 0.035 );
	medallion( "stone", 0, 0.61, -1.83, 0.25, 0.12, 8 );
	for ( const x of [ -0.73, 0.73 ] ) {
		box( "brass", [ x, 0.71, -1.42 ], [ 0.20, 0.22, 0.18 ] );
		for ( const dx of [ -0.07, 0, 0.07 ] ) box( "red", [ x + dx, 0.31, -1.53 ], [ 0.055, 0.46, 0.07 ] );
	}
	const root = { name: "root", parentIndex: -1, local: { q: [ 0, 0, 0, 1 ], t: [ 0, 0, 0 ] } };
	return avatarToGlb( {
		name: "Caravan pet loot collar",
		skeleton: { bones: [ root ] },
		parts: [ ...meshes.values() ].map( mesh => ({
			mesh: {
				...mesh,
				positions: mesh.positions.map( n => n * COLLAR_SCALE ),
				vertexCount: mesh.positions.length / 3,
				triangleCount: mesh.indices.length / 3
			}
		}) ),
		materials: new Map(
			Object.keys( MATERIAL_TILES ).map( name => [ name, {
				pngPath: fileURLToPath( new URL( "./pet-loot-textures/caravan-atlas.png", import.meta.url ) ),
				colors: { diffuse: [ 0.588, 0.588, 0.588, 1 ], ambient: [ 0.588, 0.588, 0.588, 1 ] },
				flags: 0x140
			} ] )
		),
		clips: []
	} );
}

/*
================
petLootDropModifiers

Reuse the licensed chest's ambient glow through the existing particle lane.
No copied EFP, bespoke shader or change to native chest effects is needed.
================
*/
export function petLootDropModifiers( chest ) {
	if (
		!chest?.particleModifiers?.some( row =>
			row.entries?.some( entry => entry.effectPath === "system\\item_drop_equip.efp" )
		)
	) {
		throw Error( "Pet loot glow requires the native armor chest particle modifiers" );
	}
	return { modifierSets: chest.modifierSets, particleModifiers: chest.particleModifiers };
}
