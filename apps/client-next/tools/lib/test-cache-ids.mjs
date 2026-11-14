/*
===========================================================================

test-cache-ids.mjs - test-cache input ids that hold in every checkout

The test runner records each passing file with the content hash of every
input it read. With the inputs named by absolute path, a record written in
one worktree never matched in another, so every fresh worktree ran all 766
runtime files cold (177 s on 11-14) although its files were the same bytes.
An input inside the checkout is therefore named relative to it, and resolved
against whichever checkout validates the record; an input outside it (the
shared .generated, Node itself) keeps its absolute path, which every
worktree shares. The content hash still decides reuse: a relative id only
lets two checkouts compare the same file.

===========================================================================
*/
import path from "node:path";

// Marks a checkout-relative target; never the start of an absolute path.
export const CHECKOUT_PREFIX = "~/";

const caseless = process.platform === "win32";

/*
================
encodeTarget

The id form of an absolute input path read inside root.
================
*/
export function encodeTarget( root, target ) {
	const base = path.resolve( root ) + path.sep;
	const full = path.resolve( target );
	const inside = caseless ? full.toLowerCase().startsWith( base.toLowerCase() ) : full.startsWith( base );
	if ( !inside ) return target;
	return CHECKOUT_PREFIX + full.slice( base.length ).split( path.sep ).join( "/" );
}

/*
================
decodeTarget

The absolute path an id names when validated inside root.
================
*/
export function decodeTarget( root, target ) {
	if ( !target.startsWith( CHECKOUT_PREFIX ) ) return target;
	return path.join( path.resolve( root ), ...target.slice( CHECKOUT_PREFIX.length ).split( "/" ) );
}
