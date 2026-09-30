/*
===========================================================================

world-audio.test.mjs - published world sounds decode through the asset owner

Movement, Manyang and Jangan ambience WAVs published by the build must be the
extracted bytes and decode intact in a real browser through the production
asset owner. Needs the published assets and the extraction.

===========================================================================
*/
import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdir, writeFile, readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { launchProbeBrowser } from "../../../../scripts/lib/probeBrowser.mjs";
import { CLIENT_NEXT_BASE_URL } from "../../../../scripts/lib/probeEndpoints.mjs";
import { holdProbeRuntime } from "./helpers/hold-runtime.mjs";
import { join } from "node:path";
import { dataExtractedRoot } from "../../../../scripts/build/world/paths.mjs";

test( "published movement, Manyang and Jangan ambience WAVs decode intact through the production asset owner", {
	timeout: 90000
}, async () => {
	const rules =
		JSON.parse( await readFile( "../../.generated/client-public/assets/audio/effectsound.json", "utf8" ) ).rules;
	const profiles =
		JSON.parse( await readFile( "../../.generated/client-public/assets/audio/effectenvsnd.json", "utf8" ) )
			.profiles;
	const regions =
		JSON.parse( await readFile( "../../.generated/client-public/assets/audio/regioninfo.json", "utf8" ) ).regions;
	const names = regions.filter( r => r.entries.some( e => e.sectorX === 168 && e.sectorY === 97 ) ).map( r =>
		r.name
	);
	const ambience = profiles.filter( p => names.includes( p.name ) ).flatMap(
		p => [ ...p.ambience.day, ...p.ambience.night ]
	);
	const paths = [
		...new Set(
			[
				...rules.filter( r => r.object === "MOB_MANGNYANG" || r.object === "PLAYER" && r.event1 === "FIELD" ),
				...ambience
			].map( r => r.publicPath ).filter( Boolean )
		)
	];
	const hashes = {};
	for ( const path of paths ) {
		hashes[path] = createHash( "sha256" ).update(
			await readFile( join( dataExtractedRoot, path.split( "/sfx/" )[1] ) )
		).digest( "hex" );
	}
	const { browser, page } = await launchProbeBrowser();
	try {
		await holdProbeRuntime( page );
		await page.goto( CLIENT_NEXT_BASE_URL );
		const result = await page.evaluate( async paths => {
			const { createAssets } = await import( "/src/engine/runtime/assets/assets.ts" );
			const assets = createAssets(), context = new AudioContext(), rows = [];
			try {
				for ( const path of paths ) {
					const id = assets.request( new URL( path, location.origin ).href, 4 << 20 );
					let loaded;
					const end = performance.now() + 15000;
					while ( !(loaded = assets.take( id )) ) {
						if ( performance.now() > end ) throw Error( "Audio deadline: " + path );
						await new Promise( requestAnimationFrame );
					}
					if ( loaded.kind !== "bytes" ) throw Error( loaded.error ?? "Wrong audio result" );
					const hash = Array.from(
						new Uint8Array( await crypto.subtle.digest( "SHA-256", loaded.buffer ) ),
						x => x.toString( 16 ).padStart( 2, "0" )
					).join( "" );
					const buffer = await context.decodeAudioData( loaded.buffer );
					let peak = 0, sum = 0, invalid = 0;
					for ( let c = 0; c < buffer.numberOfChannels; c++ ) {
						for ( const x of buffer.getChannelData( c ) ) {
							if ( !Number.isFinite( x ) ) invalid++;
							peak = Math.max( peak, Math.abs( x ) );
							sum += x * x;
						}
					}
					rows.push( {
						path,
						hash,
						seconds: buffer.duration,
						channels: buffer.numberOfChannels,
						sampleRate: buffer.sampleRate,
						peak,
						rms: Math.sqrt( sum / (buffer.length * buffer.numberOfChannels) ),
						invalid
					} );
				}
				return rows;
			} finally {
				assets.dispose();
				await context.close();
			}
		}, paths );
		await mkdir( "temp/artifacts/audio-parity", { recursive: true } );
		await writeFile( "temp/artifacts/audio-parity/published-waveforms.json", JSON.stringify( result, null, 2 ) );
		assert.ok( result.length > 10 );
		for ( const row of result ) {
			assert.equal( row.hash, hashes[row.path], row.path + " matches extracted retail bytes" );
			assert.equal( row.invalid, 0 );
			assert.ok( row.seconds > 0 && row.seconds < 60 );
			assert.ok( Number.isFinite( row.peak ) && row.peak > 0 );
		}
	} finally {
		await browser.close();
	}
} );
