/*
===========================================================================

asset-backed-tests.test.mjs - CI's source lane leaves out exactly the files that read assets

ops #335 row 1: the lane ran a hand-picked 17 of 766 runtime files. It now
runs every file but the ones whose recorded inputs reach the extracted
client or a workstation-only tree; the verdict comes from those inputs.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { assetBacked, classify } from "../../tools/list-asset-backed-tests.mjs";

const record = ( ...targets ) => ({ inputs: Object.fromEntries( targets.map( ( t ) => [ "F\t" + t, "sha" ] ) ) });
const SOURCE = "~/apps/client-next/src/a.ts";

test("a file that read only checkout sources is asset-free", () => {
	assert.equal( assetBacked( record( SOURCE, "~/node_modules/x/index.js" ) ), false );
});

test("an input outside the checkout or in a workstation-only tree makes it asset-backed", () => {
	assert.equal( assetBacked( record( SOURCE, "C:/sro/extracted/Data_extracted/res/a.bin" ) ), true );
	for ( const tree of [ ".generated", ".state", "temp", ".tools" ] ) {
		assert.equal( assetBacked( record( SOURCE, `~/${tree}/a.bin` ) ), true, tree );
	}
	assert.equal( assetBacked( record( "~/apps/server/.generated/game-data/a.json" ) ), true );
});

test("a file without a record keeps its previous verdict, and a removed file drops out", () => {
	const records = {
		"apps\\client-next\\tests\\runtime\\reads-assets.test.mjs": record( "~/.generated/a.bin" ),
		"apps/client-next/tests/runtime/pure.test.mjs": record( SOURCE ),
		"apps/client-next/tests/architecture/reads-assets.test.mjs": record( "~/.generated/a.bin" )
	};
	const files = [ "pure.test.mjs", "reads-assets.test.mjs", "spawns.test.mjs", "spawns-pure.test.mjs" ];
	const previous = [ "spawns.test.mjs", "deleted.test.mjs", "pure.test.mjs" ];
	assert.deepEqual( classify( { records, files, previous } ), [ "reads-assets.test.mjs", "spawns.test.mjs" ] );
});
