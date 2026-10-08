/*
===========================================================================

assetPackLayout.mjs - which assets share a pack, stable across builds

A pack is a byte range of many assets; its URL names its slot and content
hash, and browsers cache it by that URL. Cutting each group's path-sorted
files into target-sized chunks moved every later asset into a different
pack whenever one was inserted: a 17 MiB model addition re-shipped 366 MiB
of unchanged models to the host and to every player.

The layout is planned against a baseline index (the live publication for a
release, else the previous local build): a baseline pack whose members are
all present and unchanged keeps its members, their order and its slot, so
it rebuilds to the same bytes and URL. Everything else - new and changed
assets, and the survivors of a pack that lost or changed a member - is
packed into fresh slots. A group whose pack count drifts past its ideal by
more than MAX_PACK_SLACK, or whose target size changed, is repacked whole,
so the waste of underfilled packs stays bounded.

===========================================================================
*/

// Underfilled packs a group may carry before it is repacked whole.
export const MAX_PACK_SLACK = 3;

const PACK_SLOT = /-(\d{3,})-[0-9a-f]{12}\.bin$/;

/**
 * @typedef {{ publicPath: string, sha256: string, bytes: number, mime: string }} LayoutFile
 * @typedef {{ slot: number, dir: string, sha256: string, members: { path: string, sha256: string, length: number, mime: string }[] }} BaselinePack
 * @typedef {{ slot: number, files: LayoutFile[], kept: boolean, dir?: string, sha256?: string }} PlannedPack
 */

/*
================
packSlotOf

The slot number a pack path carries ("game-models-003-<hash>.bin" -> 3).
================
*/
export function packSlotOf( packPath ) {
	const match = PACK_SLOT.exec( packPath );
	return match ? Number( match[1] ) : null;
}

/*
================
baselinePacksOf

One group's packs from a published or local index, members in pack order.
ownDir is the public folder this build writes fresh packs to. Null when
the index has no usable layout for the group.
================
*/
export function baselinePacksOf( index, groupName, ownDir ) {
	const group = index?.groups?.find( ( candidate ) => candidate.name === groupName );
	if ( !group || !Array.isArray( index.assets ) ) return null;
	const byPath = new Map();
	for ( const pack of group.packs ?? [] ) {
		const slot = packSlotOf( pack.path );
		if ( slot === null ) return null;
		// A partial builder's pack lives in its own folder (incremental/.../slots):
		// keeping it means keeping that folder too.
		byPath.set( pack.path, {
			slot,
			dir: pack.path.slice( 0, pack.path.lastIndexOf( "/" ) ),
			sha256: pack.sha256,
			members: []
		} );
	}
	for ( const asset of index.assets ) {
		const pack = byPath.get( asset.packPath );
		if ( pack ) pack.members.push( asset );
	}
	for ( const pack of byPath.values() ) {
		pack.members.sort( ( left, right ) => left.offset - right.offset );
		pack.members = pack.members.map( ( member ) => ({
			path: member.path,
			sha256: member.sha256,
			length: member.length,
			mime: member.mime
		}) );
	}
	return { targetBytes: group.targetBytes, dir: ownDir, packs: [ ...byPath.values() ] };
}

/*
================
chunkByTargetBytes

Path-ordered files cut into packs of about targetBytes; a file larger than
the target gets a pack of its own.
================
*/
export function chunkByTargetBytes( files, targetBytes ) {
	const chunks = [];
	let current = [], currentBytes = 0;
	for ( const file of files ) {
		if ( current.length > 0 && currentBytes + file.bytes > targetBytes ) {
			chunks.push( current );
			current = [];
			currentBytes = 0;
		}
		current.push( file );
		currentBytes += file.bytes;
	}
	if ( current.length > 0 ) chunks.push( current );
	return chunks;
}

/*
================
fullLayout
================
*/
function fullLayout( files, targetBytes ) {
	return chunkByTargetBytes( files, targetBytes ).map( ( chunk, i ) => ({ slot: i + 1, files: chunk, kept: false }) );
}

/*
================
planPackLayout

files are the group's current files sorted by path; baseline is
baselinePacksOf()'s answer or null. Returns the packs in slot order.
================
*/
/** @returns {PlannedPack[]} */
export function planPackLayout( { files, baseline, targetBytes } ) {
	if ( !baseline || baseline.targetBytes !== targetBytes ) return fullLayout( files, targetBytes );
	const current = new Map( files.map( ( file ) => [ file.publicPath, file ] ) );
	const placed = new Set();
	/** @type {PlannedPack[]} */
	const kept = [];
	for ( const pack of baseline.packs ) {
		const members = pack.members.map( ( member ) => current.get( member.path ) );
		const intact = members.length > 0 && members.every( ( file, i ) => {
			const member = pack.members[i];
			return file && file.sha256 === member.sha256 && file.bytes === member.length && file.mime === member.mime;
		} );
		if ( !intact ) continue;
		kept.push( {
			slot: pack.slot,
			files: /** @type {LayoutFile[]} */ (members),
			kept: true,
			dir: pack.dir,
			sha256: pack.sha256
		} );
		for ( const file of members ) placed.add( file.publicPath );
	}
	const loose = files.filter( ( file ) => !placed.has( file.publicPath ) );
	const fresh = chunkByTargetBytes( loose, targetBytes );
	const totalBytes = files.reduce( ( sum, file ) => sum + file.bytes, 0 );
	const ideal = Math.max( 1, Math.ceil( totalBytes / targetBytes ) );
	if ( kept.length + fresh.length > ideal + MAX_PACK_SLACK ) return fullLayout( files, targetBytes );

	// Fresh packs go to the builder's own folder; only its slots there are taken.
	const used = new Set( kept.filter( ( pack ) => pack.dir === baseline.dir ).map( ( pack ) => pack.slot ) );
	let next = 1;
	const planned = [ ...kept ];
	for ( const chunk of fresh ) {
		while ( used.has( next ) ) next++;
		used.add( next );
		planned.push( { slot: next, files: chunk, kept: false } );
	}
	return planned.sort( ( left, right ) =>
		left.slot - right.slot || (left.dir ?? "").localeCompare( right.dir ?? "" )
	);
}
