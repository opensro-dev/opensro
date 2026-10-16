/*
===========================================================================
catalog.mjs - read-only resource reconciliation and review identity

Literal references are leads, never a claim that a resource is implemented.
Native and published trees remain under the existing path authorities.
===========================================================================
*/
import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { CLIENT_PUBLIC_ROOT } from "../../../../scripts/lib/generatedRoot.mjs";
import { clientV150ResinfoRoot, mediaExtractedRoot } from "../../../../scripts/build/world/paths.mjs";

const app = fileURLToPath( new URL( "../../", import.meta.url ) );
const checkout = path.resolve( app, "../.." );

/*
================
walk
================
*/
function walk( directory, extensions ) {
	if ( !fs.existsSync( directory ) ) return [];
	return fs.readdirSync( directory, { withFileTypes: true } ).flatMap( row => {
		const full = path.join( directory, row.name );
		return row.isDirectory() ?
			walk( full, extensions ) :
			extensions.includes( path.extname( row.name ) ) ?
			[ full ] :
			[];
	} ).sort();
}

/*
================
hash
================
*/
function hash( value ) {
	return createHash( "sha256" ).update( value ).digest( "hex" );
}

/*
================
fileHash
================
*/
function fileHash( file ) {
	return fs.existsSync( file ) ? hash( fs.readFileSync( file ) ) : null;
}

/*
================
readCatalog

Recomputed on demand so a changed source or asset cannot retain an approval.
The font hash covers atlas metadata; image bytes are not certified by it.
================
*/
export function readCatalog() {
	const sources = [ ...walk( path.join( app, "src" ), [ ".ts", ".css" ] ), path.join( app, "index.html" ) ].map(
		file => ({
			file: path.relative( checkout, file ).replaceAll( "\\", "/" ),
			text: fs.readFileSync( file, "utf8" )
		})
	);
	const qa = [
		...walk( path.join( app, "tools/ui-qa" ), [ ".ts", ".mjs", ".html", ".css", ".json" ] ),
		...walk( path.join( app, "tests/helpers" ), [ ".mjs", ".mts" ] ).filter( file =>
			path.basename( file ).startsWith( "ui-entry-" )
		)
	];
	const layouts = path.join( CLIENT_PUBLIC_ROOT, "assets/cif/layouts" );
	const native = walk( clientV150ResinfoRoot, [ ".txt" ] );
	const published = walk( layouts, [ ".json" ] );
	const names = [ ...new Set( [ ...native, ...published ].map( file => path.parse( file ).name ) ) ].sort();
	const resources = names.map( name => {
		const source = native.find( file => path.parse( file ).name === name );
		const output = published.find( file => path.parse( file ).name === name );
		const layout = output ? JSON.parse( fs.readFileSync( output, "utf8" ) ) : null;
		const references = sources.filter( row => row.text.toLowerCase().includes( name.toLowerCase() ) ).map( row =>
			row.file
		);
		return {
			name,
			nativeHash: source ? fileHash( source ) : null,
			publishedHash: output ? fileHash( output ) : null,
			url: output ? `/assets/cif/layouts/${path.basename( output )}` : null,
			sections: (layout?.sections ?? []).map( section => ({
				name: section.name,
				nodes: (section.nodes ?? []).map( ( node, occurrence ) => ({
					id: node.id,
					name: node.name,
					type: node.type,
					rect: node.rect,
					occurrence
				}) )
			}) ),
			references
		};
	} );
	const codeSha = execFileSync( "git", [ "rev-parse", "HEAD" ], { cwd: checkout, encoding: "utf8" } ).trim();
	const dirty = execFileSync( "git", [ "status", "--porcelain" ], { cwd: checkout, encoding: "utf8" } ).trim();
	const sourceHash = hash(
		sources.map( row => row.file + "\0" + row.text ).join( "\0" ) +
			qa.map( file => path.relative( app, file ) + "\0" + fs.readFileSync( file, "utf8" ) ).join( "\0" )
	);
	const assetHash = hash( JSON.stringify( resources.map( row => [ row.name, row.nativeHash, row.publishedHash ] ) ) );
	const defineHash = fileHash( path.join( mediaExtractedRoot, "config/define.txt" ) );
	const fontHash = fileHash( path.join( CLIENT_PUBLIC_ROOT, "assets/fonts/native-ui-font-atlas.json" ) );
	const identity = { codeSha, sourceHash, assetHash, defineHash, fontHash };
	return {
		identity: { ...identity, fingerprint: hash( JSON.stringify( identity ) ), dirty: !!dirty },
		roots: { checkout, native: clientV150ResinfoRoot, published: layouts },
		limitations:
			"Literal references are triage only. Authored sections are not runtime windows. Font metadata hash does not certify every image byte.",
		resources
	};
}
