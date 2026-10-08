/**
 * The client admission contract for a complete or partial pack index, enforced
 * at build time. It mirrors apps/client-next .../packs/index/index.ts: a
 * manifest that fails here is rejected wholesale by the client ("Missing pack
 * or duplicate asset") and then every asset request fails. Every writer must
 * pass this check: buildAssetPacks, the sparse group refresh, and
 * publishAssetPackManifest (the merge boundary for all incremental publishers).
 */
import { storedLength, validStoredForm } from "./shared/packFormat.mjs";

const MAX_CLIENT_PACK_BYTES = 64 << 20;

/** Client path admission (index.ts `path`): /assets/ rooted, no escapes or URL syntax. */
function assertClientPath( value, label ) {
	if ( typeof value !== "string" || !value ) throw new Error( `Asset pack manifest ${label} is not a path.` );
	const lower = value.toLowerCase();
	if (
		!lower.startsWith( "/assets/" ) ||
		/[\\%?#]/.test( lower ) ||
		lower.split( "/" ).some( ( part ) => part === "." || part === ".." )
	) {
		throw new Error( `Asset pack manifest ${label} is not client-admissible: ${value}` );
	}
}

export function validateAssetPackIndex( index ) {
	const groupStats = new Map();
	const packStats = new Map();

	for ( const group of index.groups ) {
		if ( groupStats.has( group.name ) ) {
			throw new Error( `Asset pack manifest repeats group ${group.name}.` );
		}

		const packAssetCount = group.packs.reduce( ( sum, pack ) => sum + pack.assetCount, 0 );
		if ( packAssetCount !== group.assetCount ) {
			throw new Error(
				`Asset pack manifest group ${group.name} asset count mismatch: group=${group.assetCount}, packs=${packAssetCount}.`
			);
		}

		groupStats.set( group.name, {
			assetCount: 0,
			totalBytes: 0,
			expectedAssetCount: group.assetCount,
			expectedTotalBytes: group.totalBytes
		} );

		for ( const pack of group.packs ) {
			assertClientPath( pack.path, `pack path` );
			if ( !/^\/assets\/packs\/.+\.bin$/i.test( pack.path ) ) {
				throw new Error( `Asset pack manifest pack ${pack.path} is not a /assets/packs/*.bin identity.` );
			}
			if ( !Number.isSafeInteger( pack.bytes ) || pack.bytes < 12 || pack.bytes > MAX_CLIENT_PACK_BYTES ) {
				throw new Error(
					`Asset pack manifest pack ${pack.path} exceeds the client admission budget (${pack.bytes} bytes).`
				);
			}
			if ( packStats.has( pack.path ) ) {
				throw new Error( `Asset pack manifest repeats pack ${pack.path}.` );
			}
			packStats.set( pack.path, {
				group: group.name,
				assetCount: 0,
				expectedAssetCount: pack.assetCount,
				ranges: []
			} );
		}
	}

	const seenAssets = new Set();
	for ( const asset of index.assets ) {
		assertClientPath( asset.path, `asset path` );
		// Case-folded like the client, so two groups can never both claim one path.
		const key = asset.path.toLowerCase();
		if ( seenAssets.has( key ) ) {
			throw new Error(
				`Asset pack manifest repeats asset ${asset.path} (group ${asset.group}); one path must have exactly one owning group.`
			);
		}
		seenAssets.add( key );

		const pack = packStats.get( asset.packPath );
		if ( !pack ) {
			throw new Error( `Asset pack manifest asset ${asset.path} references missing pack ${asset.packPath}.` );
		}
		if ( pack.group !== asset.group ) {
			throw new Error(
				`Asset pack manifest asset ${asset.path} group ${asset.group} does not own ${asset.packPath}.`
			);
		}

		const group = groupStats.get( asset.group );
		if ( !group ) {
			throw new Error( `Asset pack manifest asset ${asset.path} references missing group ${asset.group}.` );
		}

		pack.assetCount += 1;
		if ( !validStoredForm( asset ) ) {
			throw new Error( `Asset pack manifest asset ${asset.path} has an invalid stored form.` );
		}
		// Ranges cover the stored bytes; group totals stay the decoded sizes.
		pack.ranges.push( { path: asset.path, offset: asset.offset, end: asset.offset + storedLength( asset ) } );
		group.assetCount += 1;
		group.totalBytes += asset.length;
	}

	for ( const [groupName, group] of groupStats ) {
		if ( group.assetCount !== group.expectedAssetCount || group.totalBytes !== group.expectedTotalBytes ) {
			throw new Error(
				`Asset pack manifest group ${groupName} totals mismatch: expected ${group.expectedAssetCount}/${group.expectedTotalBytes}, got ${group.assetCount}/${group.totalBytes}.`
			);
		}
	}

	for ( const [packPath, pack] of packStats ) {
		if ( pack.assetCount !== pack.expectedAssetCount ) {
			throw new Error(
				`Asset pack manifest pack ${packPath} asset count mismatch: expected ${pack.expectedAssetCount}, got ${pack.assetCount}.`
			);
		}

		pack.ranges.sort( ( left, right ) => left.offset - right.offset );
		let previousEnd = 0;
		for ( const range of pack.ranges ) {
			if ( range.offset < previousEnd ) {
				throw new Error( `Asset pack manifest pack ${packPath} has overlapping range for ${range.path}.` );
			}
			previousEnd = range.end;
		}
	}
}
