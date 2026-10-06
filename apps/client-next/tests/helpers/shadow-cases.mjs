/*
===========================================================================

shadow-cases.mjs - deterministic moving receivers over submitted terrain

Fine/coarse LODs, negative cells, nonflat heights and duplicate overlays.
The frozen outputs were generated from a623a6f5 before the allocation fix.

===========================================================================
*/
import "./native-source-loader.mjs";
const { shadowProjection } = await import( "../../src/engine/foundation/rendering/character-shadow.ts" );
const { terrainCellKey } = await import( "../../src/engine/foundation/rendering/terrain-interaction.ts" );
/*
================
shadowCases
================
*/
export function shadowCases() {
	const scenes = [];
	for ( const step of [ 20, 40, 80, 160 ] ) {
		const surfaces = new Map();
		for ( let cz = -1; cz <= 1; cz++ ) {
			for ( let cx = -1; cx <= 1; cx++ ) {
				const positions = [], indices = [], n = 320 / step + 1;
				for ( let z = 0; z < n; z++ ) {
					for ( let x = 0; x < n; x++ ) {
						positions.push(
							cx * 320 + x * step,
							Math.sin( x * .7 + cx ) * 6 + Math.cos( z * .3 + cz ) * 9,
							cz * 320 + z * step
						);
					}
				}
				for ( let z = 0; z < n - 1; z++ ) {
					for ( let x = 0; x < n - 1; x++ ) {
						const a = z * n + x;
						indices.push( a, a + n, a + 1, a + 1, a + n, a + n + 1 );
					}
				}
				const surface = {
					positions: new Float32Array( positions ),
					indices: new Uint32Array( indices ),
					start: 0,
					count: indices.length
				};
				surfaces.set( terrainCellKey( cx, cz ), [ surface, surface ] );
			}
		}
		scenes.push( surfaces );
	}
	const cases = [];
	for ( let i = 0; i < 240; i++ ) {
		cases.push( {
			surface: scenes[i % 4],
			projection: shadowProjection(
				[ Math.sin( i * .07 ) * 210 + 100, i % 13, Math.cos( i * .05 ) * 230 + 100 ],
				20 + i % 70
			),
			blob: i % 5 === 0 ? 15 + i % 50 : undefined
		} );
	}
	return cases;
}
