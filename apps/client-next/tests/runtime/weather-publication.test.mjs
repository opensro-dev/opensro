import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { parseWeatherEvents } from "../../../../scripts/build/char/weatherEvents.mjs";
import { npcManifestModels } from "../../../../scripts/build/shared/npcManifest.mjs";
test("native characterInfo weather flags survive BOM, bit fields and unrelated sections", () => {
	const row = ( name, flags ) =>
		[ name, name, 1, "none", "none", "none", "none", "none", "0,0,0", "none", "none", flags ].join( "\t" );
	const text = "\uFEFF#section\tcharacterInfo\n" + row( "A", 1 ) + "\n" + row( "B", "2,path" ) + "\n" +
		row( "C", "3,path" ) + "\n#section\tskillaniset\nBAD";
	assert.deepEqual( [ ...parseWeatherEvents( text ) ], [ [ "A", true ], [ "B", false ], [ "C", true ] ] );
	assert.throws( () => parseWeatherEvents( "#section\tcharacterInfo\nINVALID" ) );
});
test("published Kerberos metadata and all four native weather sounds are in the live pack manifest", async () => {
	const root = CLIENT_PUBLIC_ROOT,
		manifest = JSON.parse( await readFile( root + "/assets/npc/manifest.json", "utf8" ) );
	const rows = Object.values( npcManifestModels( manifest ) );
	assert.equal( rows.find( row => row.codename === "MOB_EU_KERBEROS" )?.eventRain, true );
	const packs = JSON.parse( await readFile( root + "/assets/packs/manifest.json", "utf8" ) );
	for ( const name of [ "lightning1", "lightning2", "lightning3", "rain1" ] ) {
		assert.ok( packs.assets.some( row => row.path === "/assets/audio/sfx/prim/snd/etc/" + name + ".wav" ) );
	}
});
