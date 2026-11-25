/*
===========================================================================

publishedPortraits.mjs - the catalogue's monster stills, from the published family

The asset build already draws every monster reference through the
production viewer and publishes the stills as one content-addressed family
(char/buildMonsterPortraitAssets.mjs, /assets/npc/hunting-portraits/: a
manifest of [refObjId, path] rows, 384px transparent PNGs, each named by
its own sha256). The catalogue drew all of them a second time, through the
same viewer URL and size, in its own headless browser: 790 stills and
about 300 s of every release whose client had changed, since its cache key
covered the whole viewer page (opensro #4337, #4349; build-waste 36c). The
family's header named this switch as the follow-up.

So the catalogue reads the family from the served release instead: the
manifest, then each monster's still, checked against the sha256 in its own
name. A monster the family does not list, or a still that does not match
its name, is a failure the caller names, never a guess.

===========================================================================
*/
import { createHash } from "node:crypto";
import { mkdirSync, writeFileSync } from "node:fs";
import path from "node:path";

// The family's public prefix and manifest (buildMonsterPortraitAssets PREFIX).
export const PORTRAIT_PREFIX = "assets/npc/hunting-portraits/";
const MANIFEST_FORMAT = "sro-hunting-portraits";
const MANIFEST_VERSION = 1;
// A published still is named <sha256>.png.
const STILL_NAME = /^\/assets\/npc\/hunting-portraits\/([0-9a-f]{64})\.png$/;

/*
================
fetchServed

The bytes the served release answers for name under base.
================
*/
async function fetchServed( base, name ) {
	const response = await fetch( new URL( name, base ) );
	if ( !response.ok ) throw Error( `portraits: ${name} answered ${response.status}` );
	return Buffer.from( await response.arrayBuffer() );
}

/*
================
readPortraitManifest

The family's rows as a map of refObjId to published path. A manifest of
another format or version is refused: its rows would be read wrong.
================
*/
export function readPortraitManifest( bytes ) {
	const manifest = JSON.parse( bytes.toString( "utf8" ) );
	if ( manifest.format !== MANIFEST_FORMAT || manifest.version !== MANIFEST_VERSION ) {
		throw Error(
			`portraits: manifest is ${manifest.format} v${manifest.version}, not ${MANIFEST_FORMAT} v${MANIFEST_VERSION}`
		);
	}
	return new Map( manifest.rows.map( ( [refObjId, published] ) => [ refObjId, published ] ) );
}

/*
================
publishedPortraits

Writes each monster's still into workDir as <id>.png. Returns drawn (id to
file) and failed ({ id, reason }), the shape the catalogue's portrait
stage reads. A still shared by several monsters is fetched once.
================
*/
export async function publishedPortraits( { viewerBase, monsters, workDir, fetchBytes = fetchServed } ) {
	const rows = readPortraitManifest( await fetchBytes( viewerBase, PORTRAIT_PREFIX + "manifest.json" ) );
	mkdirSync( workDir, { recursive: true } );
	const drawn = new Map(), failed = [], stills = new Map();
	for ( const monster of monsters ) {
		const published = rows.get( monster.id );
		const named = published && STILL_NAME.exec( published );
		if ( !named ) {
			failed.push( { id: monster.id, reason: published ? "unnamed" : "unpublished" } );
			continue;
		}
		let bytes = stills.get( published );
		if ( !bytes ) {
			bytes = await fetchBytes( viewerBase, published.slice( 1 ) );
			if ( createHash( "sha256" ).update( bytes ).digest( "hex" ) !== named[1] ) {
				failed.push( { id: monster.id, reason: "mismatch" } );
				continue;
			}
			stills.set( published, bytes );
		}
		const file = path.join( workDir, monster.id + ".png" );
		writeFileSync( file, bytes );
		drawn.set( monster.id, file );
	}
	return { drawn, failed };
}
