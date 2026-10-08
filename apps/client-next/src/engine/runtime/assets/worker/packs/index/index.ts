import type { PackEntry, PackDescriptor } from "../internal/pack-contract";
// The pack layout version (scripts/build/shared/packFormat.mjs).
export const PACK_MAGIC = "SROPACK2", PACK_VERSION = 2;
export function createPackIndex() {
	const object = ( v: unknown ): Record<string, unknown> => {
		if ( !v || typeof v !== "object" || Array.isArray( v ) ) throw new Error( "Invalid pack object" );
		return v as Record<string, unknown>;
	};
	const array = ( v: unknown ): unknown[] => {
		if ( !Array.isArray( v ) ) throw new Error( "Invalid pack list" );
		return v;
	};
	const integer = ( v: unknown ): number => {
		if ( typeof v !== "number" || !Number.isSafeInteger( v ) || v < 0 ) throw new Error( "Invalid pack integer" );
		return v;
	};
	const text = ( v: unknown ): string => {
		if ( typeof v !== "string" || !v ) throw new Error( "Invalid pack string" );
		return v;
	};
	function path( v: unknown ): string {
		const s = text( v ).toLowerCase();
		if (
			!s.startsWith( "/assets/" ) || s.includes( "\\" ) || s.includes( "%" ) || s.includes( "?" ) ||
			s.includes( "#" ) || s.split( "/" ).some( p => p === "." || p === ".." )
		) throw new Error( "Invalid asset path" );
		return s;
	}
	function hash( v: unknown ): string {
		const s = text( v ).toLowerCase();
		if ( !/^[a-f0-9]{64}$/.test( s ) ) throw new Error( "Invalid pack digest" );
		return s;
	}
	function entry( value: unknown, packPath: string ): PackEntry {
		const v = object( value );
		return {
			publicPath: text( v.path ),
			path: path( v.path ),
			packPath,
			offset: integer( v.offset ),
			length: integer( v.length ),
			mime: text( v.mime ),
			sha256: hash( v.sha256 ),
			...(v.animationSources === undefined || v.animationDigest !== v.sha256 ?
				{} :
				{ animationSources: array( v.animationSources ).map( text ) }),
			...(v.stored === undefined ? {} : { stored: stored( v.stored, integer( v.length ) ) }),
			span: v.stored === undefined ? integer( v.length ) : stored( v.stored, integer( v.length ) ).length
		};
	}
	// SROPACK2 (scripts/build/shared/packFormat.mjs): a member stored compressed occupies stored.length
	// bytes at its offset and decodes to length bytes; anything that does not save bytes is invalid.
	function stored( value: unknown, length: number ): NonNullable<PackEntry["stored"]> {
		const v = object( value ), n = integer( v.length );
		if ( v.encoding !== "gzip" || n < 1 || n >= length || Object.keys( v ).length !== 2 ) {
			throw Error( "Invalid stored pack member" );
		}
		return { length: n, encoding: "gzip" };
	}

	function ranges( entries: PackEntry[], capacity: number ) {
		let end = 0;
		for ( const e of [ ...entries ].sort( ( a, b ) => a.offset - b.offset ) ) {
			if ( e.offset < end || e.offset + e.span > capacity ) {
				throw new Error( "Overlapping or out-of-bounds asset range" );
			}
			end = e.offset + e.span;
		}
	}
	return {
		manifest( value: unknown ) {
			const v = object( value );
			if ( v.version !== PACK_VERSION ) throw new Error( "Unsupported pack manifest" );
			const packs = new Map<string, PackDescriptor>(),
				assets = new Map<string, PackEntry>(),
				groups = new Set<string>();
			for ( const raw of array( v.groups ) ) {
				const group = object( raw ), name = text( group.name );
				if ( groups.has( name ) ) throw new Error( "Duplicate pack group" );
				groups.add( name );
				let count = 0;
				for ( const rawPack of array( group.packs ) ) {
					const p = object( rawPack ), key = path( p.path );
					if ( !key.startsWith( "/assets/packs/" ) || !key.endsWith( ".bin" ) || packs.has( key ) ) {
						throw new Error( "Invalid or duplicate pack identity" );
					}
					const bytes = integer( p.bytes ), assetCount = integer( p.assetCount );
					if ( bytes < 12 || bytes > (64 << 20) ) throw new Error( "Pack exceeds admission budget" );
					packs.set( key, {
						path: key,
						bytes,
						assetCount,
						sha256: hash( p.sha256 ),
						entries: [],
						...(typeof group.load === "string" ? { load: group.load } : {})
					} );
					count += assetCount;
				}
				if ( count !== integer( group.assetCount ) ) throw new Error( "Pack group count mismatch" );
			}
			for ( const raw of array( v.assets ) ) {
				const row = object( raw ), e = entry( row, path( row.packPath ) ), pack = packs.get( e.packPath );
				if ( !pack || assets.has( e.path ) ) throw new Error( "Missing pack or duplicate asset" );
				assets.set( e.path, e );
				pack.entries.push( e );
			}
			for ( const pack of packs.values() ) {
				if ( pack.entries.length !== pack.assetCount ) throw new Error( "Pack entry count mismatch" );
				ranges( pack.entries, pack.bytes );
			}
			const animationManifests = [ ...assets.values() ].filter( e =>
				/^\/assets\/world\/[^/]+\/animated-objects\.json(?:\.gz)?$/.test( e.path )
			);
			const animations = animationManifests.every( e => e.animationSources !== undefined ) ?
				new Map<string, string[]>() :
				null;
			if ( animations ) {
				for ( const e of animationManifests ) {
					for ( const source of e.animationSources! ) {
						const key = source.toLowerCase(), rows = animations.get( key ) ?? [];
						if ( !rows.includes( e.publicPath ) ) rows.push( e.publicPath );
						animations.set( key, rows );
					}
				}
			}
			return {
				packs,
				assets,
				animations,
				animationManifests: animationManifests.map( e => e.publicPath ),
				animationOrder: new Map( animationManifests.map( ( e, i ) => [ e.publicPath, i ] ) )
			};
		},
		header( bytes: Uint8Array, pack: PackDescriptor, partial = false ): number {
			if (
				(!partial && bytes.length !== pack.bytes) || bytes.length < 12 ||
				new TextDecoder().decode( bytes.subarray( 0, 8 ) ) !== PACK_MAGIC
			) throw new Error( "Invalid pack identity" );
			const start = 12 + new DataView( bytes.buffer, bytes.byteOffset, bytes.byteLength ).getUint32( 8, true );
			if ( start > bytes.length ) throw new Error( "Truncated pack header" );
			const header = object(
				JSON.parse( new TextDecoder( "utf-8", { fatal: true } ).decode( bytes.subarray( 12, start ) ) )
			);
			if ( header.format !== "sro-asset-pack" || header.version !== PACK_VERSION ) {
				throw new Error( "Unsupported pack header" );
			}
			const files = array( header.files ).map( v => entry( v, pack.path ) );
			if ( files.length !== pack.assetCount ) throw new Error( "Pack header count mismatch" );
			if ( partial && bytes.length !== start ) throw new Error( "Invalid partial pack header" );
			const table = new Map<string, PackEntry>();
			for ( const e of files ) {
				if ( table.has( e.path ) ) throw new Error( "Duplicate pack header entry" );
				table.set( e.path, e );
			}
			ranges( files, pack.bytes - start );
			for ( const expected of pack.entries ) {
				const actual = table.get( expected.path );
				if (
					!actual || actual.offset !== expected.offset || actual.length !== expected.length ||
					actual.mime !== expected.mime || actual.sha256 !== expected.sha256 || actual.span !== expected.span
				) throw new Error( "Pack header disagrees with manifest" );
			}
			return start;
		}
	};
}
