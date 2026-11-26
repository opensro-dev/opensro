import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { gzipSync } from "node:zlib";
import { sha, inspect } from "./policy.mjs";
/*
===========================================================================

compression.mjs - build-time HTTP variants for the beta publication

Compression is entirely build-time. Only already inspected
application files and the mutable publication index receive HTTP
variants here; pack-member transports remain owned by the asset
publication pipeline.

===========================================================================
*/
/*
================
compressRoutes

Write the gzipped variant of every application route the manifest
does not already carry one for, and record each variant in the
manifest. Variants are content-named under encoded/, and the write
is exclusive ('wx'): a name collision fails the build instead of
overwriting a published file.
================
*/
export async function compressRoutes( root, manifest ) {
	for (
		const route of manifest.routes.filter( r =>
			!r.gzip && (r.file.startsWith( "application/" ) || r.file === "publication.json")
		)
	) {
		const entry = manifest.files.find( e => e.path === route.file ),
			raw = await readFile( path.join( root, entry.path ) ),
			bytes = gzipSync( raw, { level: 9 } );
		const file = "encoded/" + sha( bytes ) + path.extname( entry.path ) + ".gz";
		inspect( file, bytes, { application: entry.kind === "application" } );
		await mkdir( path.join( root, "encoded" ), { recursive: true } );
		await writeFile( path.join( root, file ), bytes, { flag: "wx" } );
		manifest.files.push( { path: file, length: bytes.length, sha256: sha( bytes ), kind: entry.kind } );
		route.gzip = { file, length: bytes.length, offset: 0 };
	}
}
