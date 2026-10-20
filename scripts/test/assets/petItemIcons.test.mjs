/*
===========================================================================

petItemIcons.test.mjs - every pet slot/degree exports distinct native-size art

===========================================================================
*/
import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { runPython } from "../../build/shared/pythonRun.mjs";
import { decodePngRgba } from "../lib/pngRgba.mjs";

test("all27 pet icons are distinct32x32 RGBA images with visible object detail", async () => {
	const script = fileURLToPath( new URL( "../../build/shared/pet_item_icons.py", import.meta.url ) );
	const { stdout } = await runPython( [ script ], { task: "Pet icon asset test" } );
	const icons = JSON.parse( stdout );
	assert.equal( icons.length, 27 );
	const hashes = new Set();
	for ( const icon of icons ) {
		const bytes = Buffer.from( icon.png, "base64" );
		const { width, height, rgba } = decodePngRgba( bytes );
		assert.equal( width, 32 );
		assert.equal( height, 32 );
		assert.ok( rgba.some( ( value, index ) => index % 4 !== 3 && value > 100 ), "not a blank background" );
		hashes.add( createHash( "sha256" ).update( bytes ).digest( "hex" ) );
	}
	assert.equal( hashes.size, 27, "every slot and degree has different artwork" );
});

test("all source pet skill and gear atlases are compact native-cell sheets", async () => {
	const { readFile } = await import( "node:fs/promises" );
	for ( const name of [ "pet-skills.png", "pet-skills-1.png", "pet-skills-2.png" ] ) {
		const bytes = await readFile( new URL( `../../../docs/pet-expansion/assets/${name}`, import.meta.url ) );
		const { width, height } = decodePngRgba( bytes );
		assert.equal( width, 160 );
		assert.equal( height, 128 );
		assert.ok( bytes.length < 100_000, `${name} should be a compact 160x128 sheet` );
	}
	for ( const name of [ "pet-gear-collar.png", "pet-gear-harness.png", "pet-gear-charm.png" ] ) {
		const bytes = await readFile( new URL( `../../../docs/pet-expansion/assets/${name}`, import.meta.url ) );
		const { width, height } = decodePngRgba( bytes );
		assert.equal( width, 96 );
		assert.equal( height, 96 );
		assert.ok( bytes.length < 50_000, `${name} should be a compact 96x96 sheet` );
	}
});
