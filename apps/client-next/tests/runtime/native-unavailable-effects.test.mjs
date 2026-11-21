/*
===========================================================================

native-unavailable-effects.test.mjs - tests for effects.ts, program.ts,
effect-programs.ts and resources.ts

An EFP the retail archive proves absent is published as its own file that
carries the archive evidence (#3951). The worker turns it into the empty
stored object native AFF730 retains, and the resource owner holds it
resident once instead of retrying.

Loads the TypeScript sources directly through the shared native loader
(tests/helpers/native-source-loader.mjs), so the tests exercise the same
modules the client ships, not a per-test bundle.

===========================================================================
*/
import "../helpers/native-source-loader.mjs";
import { pathToFileURL as sourceFileUrl } from "node:url";
import { test } from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";

const { createEffectDecoder } = await import(
	sourceFileUrl( "src/engine/runtime/assets/worker/effects/effects.ts" ).href
);
const { createCharacterResources } = await import(
	sourceFileUrl( "src/engine/runtime/characters/resources/resources.ts" ).href
);
const { effectProgramHeader, effectProgramFile, effectProgramModel } = await import(
	sourceFileUrl( "src/engine/foundation/assets/effect-programs.ts" ).href
);
const evidence = JSON.parse( fs.readFileSync( "../../scripts/build/reference/particle-archive.json", "utf8" ) );
const bytes = value => Buffer.from( JSON.stringify( value ) );
const absentFile = () => ({ ...effectProgramHeader(), unavailable: { archiveSha256: evidence.sha256 } });
const EMPTY = { model: { nodes: [], clips: [], images: [], primitives: [] }, imagePaths: [] };

test("archive-proven absent EFPs reproduce the retained empty native resource on repeated loads", () => {
	const decoder = createEffectDecoder(), data = bytes( absentFile() );
	for ( let repeat = 0; repeat < 2; repeat++ ) {
		for ( const path of evidence.absent ) {
			// The archive is case-blind: any spelling names the same file.
			const spelled = path.toUpperCase().replaceAll( "/", "\\" );
			assert.equal( effectProgramFile( spelled ), effectProgramFile( path ) );
			assert.deepEqual( decoder.model( decoder.program( data ), {}, spelled ), EMPTY );
		}
	}
	const { unavailable, ...missing } = absentFile();
	assert.throws( () => decoder.program( bytes( missing ) ), /Missing effect program/ );
});

test("malformed or contradictory archive certificates and foreign layouts fail closed", () => {
	const decoder = createEffectDecoder();
	for (
		const { mutate, error } of [
			{ mutate: c => c.unavailable.archiveSha256 = "unverified", error: /Invalid native unavailable/ },
			{ mutate: c => c.effect = { scale: 1 }, error: /Invalid native unavailable/ },
			{ mutate: c => c.version = effectProgramHeader().version - 1, error: /Unsupported effect program layout/ },
			{ mutate: c => c.framesPerSecond = 30, error: /Unsupported effect time base/ }
		]
	) {
		const c = absentFile();
		mutate( c );
		assert.throws( () => decoder.program( bytes( c ) ), error );
	}
});

test("native-empty effects become resident once instead of entering the failed-resource retry loop", () => {
	const decoder = createEffectDecoder(),
		data = bytes( absentFile() ),
		path = effectProgramModel( evidence.absent[0] );
	let requests = 0, loaded = 0;
	const pending = new Map();
	const assets = {
		available: () => 4,
		request( url ) {
			const result = decoder.model(
				decoder.program( data ),
				{},
				decodeURIComponent( new URL( url ).hash.slice( 1 ) )
			);
			pending.set( ++requests, { kind: "character", model: result.model, images: [] } );
			return requests;
		},
		take( id ) {
			const value = pending.get( id );
			pending.delete( id );
			return value;
		},
		cancel( id ) {
			pending.delete( id );
		}
	};
	const owner = createCharacterResources( assets, {
		setCharacterModel( url, model ) {
			assert.equal( url, path );
			assert.equal( model.primitives.length, 0 );
			loaded++;
		},
		retainCharacterModels() {}
	}, "http://fixture.invalid" );
	for ( let time = 0; time < 10; time++ ) {
		owner.begin( time );
		owner.poll();
		owner.ready( path );
		owner.retainWanted( [ path ] );
	}
	assert.equal( requests, 1 );
	assert.equal( loaded, 1 );
	assert.equal( owner.ready( path ), true );
	owner.dispose();
});
