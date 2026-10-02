/*
===========================================================================

character-hierarchy.test.mjs - tests for character-hierarchy.ts

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fc from "fast-check";

const { createCharacterHierarchy } = await import(
	sourceFileUrl( "src/engine/foundation/animation/character-hierarchy.ts" ).href
);
function reference( rows ) {
	const map = new Map( rows.map( row => [ row.gid, row ] ) );
	return new Map( rows.map( row => {
		const chain = [];
		let current = row;
		while ( current ) {
			if ( chain.includes( current ) || chain.length >= 8 ) throw Error( "cycle" );
			chain.push( current );
			const parent = current.attachment?.gid ?? current.mountedOn;
			if ( parent === undefined ) break;
			const mounted = current.attachment === undefined;
			current = map.get( parent );
			// A missing attachment parent hides the chain; a missing mount ends it.
			if ( !current && !mounted ) chain.length = 0;
		}
		return [ row.gid, chain ];
	} ) );
}
test("retained hierarchy matches full traversal through pose, parent, membership and reset sequences", () => {
	fc.assert(
		fc.property(
			fc.array( fc.array( fc.integer( { min: -2, max: 12 } ), { maxLength: 12 } ), {
				minLength: 1,
				maxLength: 60
			} ),
			sequence => {
				const owner = createCharacterHierarchy();
				let time = 0;
				for ( const links of sequence ) {
					const rows = links.map( ( parent, gid ) => ({
						gid,
						time: time++,
						model: "m",
						...(parent >= 0 ?
							gid % 2 ? { mountedOn: parent } : { attachment: { gid: parent, bone: "b" } } :
							{})
					}) );
					let expected;
					try {
						expected = reference( rows );
					} catch {
						assert.throws( () => owner.update( rows ), /Cyclic/ );
						continue;
					}
					const actual = owner.update( rows );
					assert.deepEqual( actual.chains, expected );
					const count = owner.stats().rebuilds, posed = rows.map( row => ({ ...row, time: time++ }) );
					assert.deepEqual( owner.update( posed ).chains, reference( posed ) );
					assert.equal( owner.stats().rebuilds, count );
					if ( time % 7 === 0 ) owner.reset();
				}
			}
		),
		{ seed: 2402028, numRuns: 1000 }
	);
});

test("a rider whose mount is absent stays drawn; an orphaned attachment does not", () => {
	const owner = createCharacterHierarchy();
	const rider = { gid: 1, mountedOn: 99, model: "m" },
		weapon = { gid: 2, attachment: { gid: 98, bone: "b" }, model: "m" };
	const { chains } = owner.update( [ rider, weapon ] );
	assert.deepEqual( chains.get( 1 ), [ rider ] );
	assert.deepEqual( chains.get( 2 ), [] );
});
