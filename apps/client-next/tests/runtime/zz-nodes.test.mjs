import "../helpers/native-source-loader.mjs";
import { test } from "node:test";
import { readPublishedAssetBytesSync } from "../../../../scripts/lib/publishedAsset.mjs";
import { publicRoot } from "../../../../scripts/build/world/paths.mjs";
const { createEffectPrograms } = await import( "../../src/engine/runtime/assets/worker/effects/program/program.ts" );
test( "n", () => {
	const bytes = readPublishedAssetBytesSync( "/assets/effects/programs.json", publicRoot ), d = createEffectPrograms();
	const doc = JSON.parse( new TextDecoder().decode( bytes ) );
	const keys = Array.isArray( doc.effects ) ? doc.effects.map( e => e.path ?? e.name ?? e.key ) : Object.keys( doc.effects );
	const rows = [];
	for ( const k of keys ) { try { const r = d.decode( bytes, k ); rows.push( [ r.model.nodes.length, k ] ); } catch ( e ) { if ( !/rotation axis/.test( e.message ) ) rows.push( [ -1, k + " " + e.message ] ); } }
	rows.sort( ( a, b ) => b[0] - a[0] );
	console.log( "programs", keys.length, JSON.stringify( rows.slice( 0, 8 ) ), rows.filter( r => r[0] < 0 ).length );
} );
