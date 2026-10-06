/*
===========================================================================

skill-sound-admission.test.mjs - tests for sound-selectors.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const { skillSoundRoots } = await import( sourceFileUrl( "src/engine/foundation/animation/sound-selectors.ts" ).href );

test("unrequested cyclic overrides cannot abort admission; requested cycles still fail", () => {
	const lookup = skillSoundRoots( [ "1\t0\tROOT\tGROUP", "2\t1\tCHILD\tCHILD_GROUP", "3\t3\tUNUSED\tUNUSED" ] );
	assert.deepEqual( lookup.get( 2 ), [ "ROOT", "GROUP" ] );
	assert.equal( lookup.get( 2 ), lookup.get( 1 ) );
	assert.throws( () => lookup.get( 3 ), /Cyclic/ );
	assert.throws( () => lookup.get( 3 ), /Cyclic/ );
	assert.deepEqual( lookup.get( 2 ), [ "ROOT", "GROUP" ] );
	assert.equal( lookup.get( 99 ), undefined );
	assert.deepEqual( skillSoundRoots( [ "1\t9\tA\tB" ] ).get( 1 ), [ "-", "-" ] );
	assert.throws( () => skillSoundRoots( [ "bad" ] ), /Invalid/ );
	assert.throws( () => skillSoundRoots( [ "1\t0\tA\tB", "1\t0\tA\tB" ] ), /Invalid/ );
});

test("complete published skill table admits and requested roots match uncached traversal", async () => {
	const data = JSON.parse(
		await readFile( CLIENT_PUBLIC_ROOT + "/assets/data/skillAudioData.json", "utf8" )
	);
	const rows = data.skillAudioRows,
		lookup = skillSoundRoots( rows ),
		records = new Map( rows.map( row => {
			const [id, parent, name, group] = row.split( "\t" );
			return [ Number( id ), { parent: Number( parent ), name, group } ];
		} ) );
	let resolved = 0, cycles = 0;
	for ( const id of records.keys() ) {
		let current = id, expected;
		const visited = new Set();
		for ( ;; ) {
			if ( visited.has( current ) ) {
				cycles++;
				assert.throws( () => lookup.get( id ), /Cyclic/ );
				break;
			}
			visited.add( current );
			const row = records.get( current );
			if ( !row ) {
				expected = [ "-", "-" ];
				break;
			}
			if ( !row.parent ) {
				expected = [ row.name, row.group ];
				break;
			}
			current = row.parent;
		}
		if ( expected ) {
			assert.deepEqual( lookup.get( id ), expected, `skill ${id}` );
			resolved++;
		}
	}
	assert.ok( resolved > 27000 );
	assert.equal( resolved + cycles, rows.length );
});
