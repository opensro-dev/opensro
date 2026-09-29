/*
===========================================================================

build.mjs - verified browser release assembly

Freezes application inputs and projects public assets before creating the
release archive. Private maps remain outside the deployable package.

===========================================================================
*/
import { readFile, writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "vite";
import { zstdDecompressSync } from "node:zlib";
import { files, inspect, publicIndex, safeName, sha, releaseIdentity, verifyDirectory } from "./policy.mjs";
import { RELEASE_PROTOCOL } from "../../src/engine/foundation/release/protocol.ts";
import { archiveRelease } from "./archive.mjs";
import { compressRoutes } from "./compression.mjs";
import { projectPack } from "./public-data.mjs";
import {
	backgroundInstallDocument,
	BACKGROUND_INSTALL_PUBLIC_PATH
} from "../../../../scripts/build/data/buildBackgroundInstallAsset.mjs";
const root = path.resolve( import.meta.dirname, "../.." );
/*
================
write
================
*/
const write = async ( p, b ) => {
	await mkdir( path.dirname( p ), { recursive: true } );
	await writeFile( p, b, { flag: "wx" } );
};
/*
================
freezeSource
================
*/
export async function freezeSource( base = root ) {
	const source = {};
	for ( const f of await files( path.join( base, "src" ) ) ) {
		source["src/" + f] = await readFile( path.join( base, "src", f ), "utf8" );
	}
	source["index.html"] = await readFile( path.join( base, "index.html" ), "utf8" );
	return source;
}
/*
================
buildApplication
================
*/
export async function buildApplication( { base = root, directory, source, mode = "beta" } ) {
	/*
	================
	normal
	================
	*/
	const normal = p => p.replaceAll( "\\", "/" ),
		inputs = new Map( Object.entries( source ).map( ( [n, s] ) => [ normal( path.join( base, n ) ), s ] ) );
	const maps = new Map();
	/*
	================
	plugin
	================
	*/
	const plugin = () => ({
		name: "beta-frozen-inputs",
		enforce: "pre",
		/*
		================
		resolveId
		================
		*/
		resolveId( id, importer ) {
			const p = id.startsWith( "/src/" ) ?
				path.join( base, id.slice( 1 ) ) :
				id.startsWith( "@/" ) ?
				path.join( base, "src", id.slice( 2 ) ) :
				id.startsWith( "." ) && importer ?
				path.resolve( path.dirname( importer.split( "?" )[0] ), id ) :
				id;
			if ( !path.isAbsolute( p ) ) return;
			for (
				const suffix of [ "", ".ts", ".tsx", ".js", "/index.ts" ]
			) if ( inputs.has( normal( p + suffix ) ) ) return normal( p + suffix );
		},
		/*
		================
		load
		================
		*/
		load( id ) {
			if ( id.includes( "?" ) ) return;
			const p = normal( id );
			if ( p.startsWith( normal( path.join( base, "src" ) ) + "/" ) && !inputs.has( p ) ) {
				throw Error(
					"Unfrozen source dependency " + p
				);
			}
			return inputs.get( p );
		},
		transformIndexHtml: { order: "pre", handler: () => source["index.html"] },
		/*
		================
		generateBundle
		================
		*/
		generateBundle( _options, bundle ) {
			for ( const [name, entry] of Object.entries( bundle ) ) {
				if ( name.endsWith( ".map" ) ) {
					safeName( name );
					maps.set( name, entry.source );
					delete bundle[name];
				}
			}
		}
	});
	const staging = path.resolve( directory, "../../private/compiled-" + mode );
	await mkdir( staging, { recursive: true } );
	await build( {
		root: base,
		configFile: false,
		envDir: false,
		envPrefix: [],
		mode,
		publicDir: false,
		logLevel: "warn",
		plugins: [ plugin() ],
		resolve: { alias: { "@": path.join( base, "src" ) } },
		worker: { plugins: () => [ plugin() ] },
		build: {
			outDir: staging,
			emptyOutDir: false,
			sourcemap: "hidden",
			minify: "esbuild",
			rollupOptions: { output: {} }
		},
		esbuild: { legalComments: "eof" }
	} );
	// Vite may emit worker maps after generateBundle. Final filesystem outputs
	// are the authority; intermediate compiler output never enters the web root.
	for ( const name of await files( staging ) ) {
		const bytes = await readFile( path.join( staging, name ) );
		if ( name.endsWith( ".map" ) ) maps.set( name, bytes );
		else await write( path.join( directory, name ), bytes );
	}
	for ( const name of await files( directory ) ) {
		inspect( name, await readFile( path.join( directory, name ) ), { application: true } );
	}
	return maps;
}
/*
================
buildBeta
================
*/
export async function buildBeta(
	{
		destination = path.join( root, "temp/artifacts", "beta", new Date().toISOString().replace( /[:.]/g, "-" ) ),
		assetRoot = path.resolve( root, "../../.generated/client-public" ),
		makeArchive = true,
		sourceSnapshot
	} = {}
) {
	// A fresh generation only: never delete or reuse a directory that could hold
	// someone else's artifacts. Private maps are a sibling of the deployable package.
	await mkdir( destination, { recursive: false } );
	const packageRoot = path.join( destination, "package" ), privateRoot = path.join( destination, "private" );
	const source = sourceSnapshot ? JSON.parse( await readFile( sourceSnapshot, "utf8" ) ) : await freezeSource(),
		inputHash = sha( JSON.stringify( source ) );
	await write( path.join( privateRoot, "source.json" ), JSON.stringify( source ) );
	const applicationRoot = path.join( packageRoot, "application" ),
		maps = await buildApplication( { directory: applicationRoot, source } );
	for ( const [name, map] of maps ) await write( path.join( privateRoot, "maps", name ), map );
	const manifest = {
		format: "sro-beta-release-v1",
		// The release protocol compiled into this build; release admission
		// compares it with the declared one (ops/release/client_bundle.py).
		protocol: RELEASE_PROTOCOL,
		sourceHash: inputHash,
		files: [],
		routes: [],
		excludedGroups: [],
		privateMaps: maps.size
	};
	/*
	================
	add
	================
	*/
	const add = async ( file, bytes, kind ) => {
		safeName( file );
		inspect( file, bytes, { application: kind === "application" } );
		await write( path.join( packageRoot, file ), bytes );
		manifest.files.push( { path: file, length: bytes.length, sha256: sha( bytes ), kind } );
	};
	/*
	================
	route
	================
	*/
	const route = ( { url, file, length, mime, offset = 0, encoding } ) => {
		safeName( url.slice( 1 ) );
		manifest.routes.push( { url, file, offset, length, mime, ...(encoding ? { encoding } : {}) } );
	};
	for ( const name of await files( applicationRoot ) ) {
		const bytes = await readFile( path.join( applicationRoot, name ) );
		manifest.files.push( {
			path: "application/" + name,
			length: bytes.length,
			sha256: sha( bytes ),
			kind: "application"
		} );
		route( {
			url: "/" + name,
			file: "application/" + name,
			length: bytes.length,
			mime: name.endsWith( ".js" ) ? "text/javascript" : name.endsWith( ".css" ) ? "text/css" : "text/html"
		} );
	}
	const authority = await readFile( path.join( assetRoot, "assets/packs/manifest.json" ) );
	manifest.assetAuthorityHash = sha( authority );
	const original = JSON.parse( authority ), index = publicIndex( original );
	manifest.excludedGroups = original.groups.filter( g => !index.groups.includes( g ) ).map( g => g.name );
	// Pack members are inspected even when no loose representation remains.
	const overrides = new Map();
	for ( const group of index.groups ) {
		for ( const pack of group.packs ) {
			console.log( "[beta] pack", group.name, pack.path );
			/*
			================
			resolve
			================
			*/
			const resolve = n => path.join( assetRoot, safeName( n.slice( 1 ) ) );
			let bytes;
			try {
				bytes = await readFile( resolve( pack.path ) );
			} catch ( e ) {
				if ( e.code !== "ENOENT" ) throw e;
				bytes = zstdDecompressSync( await readFile( resolve( pack.zstdPath ) ), {
					maxOutputLength: 128 << 20
				} );
			}
			if ( bytes.length !== pack.bytes || sha( bytes ) !== pack.sha256 ) {
				throw Error( "Publication pack drift " + pack.path );
			}
			const members = index.assets.filter( a => a.packPath === pack.path );
			bytes = projectPack( pack, bytes, members, overrides );
			// Zstandard files are an offline publication input. This adapter serves the
			// materialized pack and authored gzip transports; never advertise absent URLs.
			for ( const key of [ "zstdPath", "zstdBytes", "zstdLevel", "zstdWindowLog" ] ) delete pack[key];
			const file = "payload/" + pack.sha256 + ".bin";
			await add( file, bytes, "data" );
			route( { url: pack.path, file, length: bytes.length, mime: "application/octet-stream" } );
			const start = 12 + bytes.readUInt32LE( 8 ), header = JSON.parse( bytes.subarray( 12, start ) );
			if ( header.files.length !== members.length ) throw Error( "Pack membership drift" );
			for ( const a of members ) {
				const e = header.files.find( e => e.path === a.path );
				if ( !e || e.offset !== a.offset || e.length !== a.length || e.sha256 !== a.sha256 ) {
					throw Error( "Pack index drift " + a.path );
				}
				route( { url: a.path, file, length: a.length, mime: a.mime, offset: start + a.offset } );
				if (
					a.path.endsWith( ".json.gz" ) && !index.assets.some( other => other.path === a.path.slice( 0, -3 ) )
				) {
					route( {
						url: a.path.slice( 0, -3 ),
						file,
						length: a.length,
						mime: "application/json",
						offset: start + a.offset,
						encoding: "gzip"
					} );
				}
			}
		}
	}
	const transports = new Map( index.assets.filter( a => a.transport ).map( a => [ a.transport.path, a.transport ] ) );
	for ( const [url, t] of transports ) {
		const bytes = overrides.get( url ) ?? await readFile( path.join( assetRoot, safeName( url.slice( 1 ) ) ) );
		if ( bytes.length !== t.length || sha( bytes ) !== t.sha256 ) throw Error( "Transport drift " + url );
		const file = "payload/" + t.sha256 + ".gz";
		await add( file, bytes, "data" );
		route( { url, file, length: bytes.length, mime: "application/octet-stream" } );
	}
	for ( const group of index.groups ) {
		const members = index.assets.filter( a => a.group === group.name );
		group.totalBytes = members.reduce( ( n, a ) => n + a.length, 0 );
		group.assetCount = members.length;
	}
	const indexBytes = Buffer.from( JSON.stringify( index ) );
	await add( "publication.json", indexBytes, "data" );
	route( {
		url: "/assets/packs/manifest.json",
		file: "publication.json",
		length: indexBytes.length,
		mime: "application/json"
	} );
	if (
		sha( await readFile( path.join( assetRoot, "assets/packs/manifest.json" ) ) ) !== manifest.assetAuthorityHash
	) throw Error( "Asset publication changed during freeze; package not accepted" );
	// Required runtime metadata is derived from this release's routed members.
	// A loose development sidecar cannot be assumed to survive pack projection.
	if ( !manifest.routes.some( row => row.url === BACKGROUND_INSTALL_PUBLIC_PATH ) ) {
		const installBytes = Buffer.from(
			JSON.stringify( backgroundInstallDocument( manifest.routes.map( row => row.url ) ) )
		);
		const installFile = "payload/" + sha( installBytes ) + ".json";
		await add( installFile, installBytes, "data" );
		route( {
			url: BACKGROUND_INSTALL_PUBLIC_PATH,
			file: installFile,
			length: installBytes.length,
			mime: "application/json"
		} );
	}
	await compressRoutes( packageRoot, manifest );
	manifest.files.sort( ( a, b ) => a.path.localeCompare( b.path ) );
	manifest.routes.sort( ( a, b ) => a.url.localeCompare( b.url ) );
	manifest.releaseId = releaseIdentity( manifest );
	await write( path.join( packageRoot, "release.json" ), JSON.stringify( manifest, null, 2 ) );
	await verifyDirectory( packageRoot );
	if ( makeArchive ) await archiveRelease( packageRoot, path.join( destination, "client-beta.tar" ) );
	await write( path.join( privateRoot, "release-id.txt" ), manifest.releaseId );
	await write(
		path.join( privateRoot, "debug.json" ),
		JSON.stringify(
			{
				releaseId: manifest.releaseId,
				maps: Object.fromEntries( [ ...maps ].map( ( [n, b] ) => [ n, sha( b ) ] ) )
			},
			null,
			2
		)
	);
	console.log( "[beta] verified", destination, manifest.releaseId );
	return { destination, packageRoot, privateRoot, manifest };
}
if ( process.argv[1] && path.resolve( process.argv[1] ) === fileURLToPath( import.meta.url ) ) {
	await mkdir( path.join( root, "temp/artifacts/beta" ), { recursive: true } );
	await buildBeta( {
		...(process.argv[2] ? { destination: path.resolve( process.argv[2] ) } : {}),
		...(process.argv[3] === "--source" ? { sourceSnapshot: path.resolve( process.argv[4] ) } : {})
	} );
}
