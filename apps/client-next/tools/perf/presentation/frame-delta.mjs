/*
===========================================================================

frame-delta.mjs - replay frames written as changes from the frame before

A standing crowd republishes 200 actors a frame whose only change is
their clocks; written whole, one minute is gigabytes. replay.mjs writes
each frame as frameDelta( previous, frame ) and diff.mjs rebuilds every
frame with applyDelta before comparing, so the comparison and its report
are on whole frames. Two outputs whose frames are equal up to frame N
have equal lines up to N, and the first unequal line is the first
unequal frame.

The values are crowd-capture encodings (plain JSON). A patch is an object
with the DELTA key (an object patch: changed keys, REMOVED for dropped
ones), the LENGTH key (an array patch: the new length and changed
indexes), the GIDS key (an array of records patched by gid: the new
gid order and the changed records) or the REUSE key (an array of strings:
old indexes and new strings); anything else is the new value itself. Key order is not kept
(a rebuilt object lists its new keys last); diff.mjs compares keys as
sets.

===========================================================================
*/
const DELTA = "$delta";
const LENGTH = "$length";
const REMOVED = "$removed";
const GIDS = "$gids";
const CHANGES = "$changes";
const REUSE = "$reuse";
const SAME = Symbol( "same" );

/*
================
strings

A non-empty array of strings (the retained model ids): a change is
written as the new list, each entry an index into the old list or a new
string, so one id entering does not rewrite the long ids after it.
================
*/
function strings( value ) {
	return Array.isArray( value ) && value.length > 0 && value.every( item => typeof item === "string" );
}

/*
================
keyed

An array of records with distinct numeric gids (actors, rows): patched
by gid, so a record entering or leaving does not rewrite its successors.
================
*/
function keyed( value ) {
	if ( !Array.isArray( value ) || value.length === 0 ) return false;
	const seen = new Set();
	for ( const item of value ) {
		if ( item === null || typeof item !== "object" || typeof item.gid !== "number" || seen.has( item.gid ) ) {
			return false;
		}
		seen.add( item.gid );
	}
	return true;
}

/*
================
plainObject
================
*/
function plainObject( value ) {
	return value !== null && typeof value === "object" && !Array.isArray( value );
}

/*
================
delta

SAME when next equals previous, else the patch that turns previous into
next.
================
*/
function delta( previous, next ) {
	if ( previous === next ) return SAME;
	if ( keyed( previous ) && keyed( next ) ) {
		const before = new Map( previous.map( item => [ item.gid, item ] ) ), changes = {};
		let changed = previous.length !== next.length;
		for ( let i = 0; i < next.length; i++ ) {
			const item = next[i], old = before.get( item.gid );
			changed ||= previous[i]?.gid !== item.gid;
			const patch = old ? delta( old, item ) : item;
			if ( patch !== SAME ) {
				changes[item.gid] = patch;
				changed = true;
			}
		}
		return changed ? { [GIDS]: next.map( item => item.gid ), [CHANGES]: changes } : SAME;
	}
	if ( strings( previous ) && strings( next ) ) {
		if ( previous.length === next.length && previous.every( ( item, i ) => item === next[i] ) ) return SAME;
		const at = new Map( previous.map( ( item, i ) => [ item, i ] ) );
		return { [REUSE]: next.map( item => at.get( item ) ?? item ) };
	}
	if ( Array.isArray( previous ) && Array.isArray( next ) ) {
		const patch = {};
		let changed = previous.length !== next.length;
		for ( let i = 0; i < next.length; i++ ) {
			const item = i < previous.length ? delta( previous[i], next[i] ) : next[i];
			if ( item !== SAME ) {
				patch[i] = item;
				changed = true;
			}
		}
		return changed ? { [LENGTH]: next.length, ...patch } : SAME;
	}
	if ( plainObject( previous ) && plainObject( next ) ) {
		const patch = {}, removed = [];
		let changed = false;
		for ( const key of Object.keys( next ) ) {
			const item = Object.hasOwn( previous, key ) ? delta( previous[key], next[key] ) : next[key];
			if ( item !== SAME ) {
				patch[key] = item;
				changed = true;
			}
		}
		for ( const key of Object.keys( previous ) ) if ( !Object.hasOwn( next, key ) ) removed.push( key );
		if ( removed.length ) patch[REMOVED] = removed;
		return changed || removed.length ? { [DELTA]: patch } : SAME;
	}
	return next;
}

/*
================
frameDelta

The line for a frame: its patch from the frame before ({} before the
first).
================
*/
export function frameDelta( previous, frame ) {
	const patch = delta( previous ?? {}, frame );
	return patch === SAME ? { [DELTA]: {} } : patch;
}

/*
================
applyDelta

The frame a line describes, given the frame before it. Unchanged
subtrees are shared with the previous frame, never mutated.
================
*/
export function applyDelta( previous, patch ) {
	if ( plainObject( patch ) && Object.hasOwn( patch, REUSE ) ) {
		return patch[REUSE].map( item => typeof item === "number" ? previous[item] : item );
	}
	if ( plainObject( patch ) && Object.hasOwn( patch, GIDS ) ) {
		const before = new Map( (Array.isArray( previous ) ? previous : []).map( item => [ item.gid, item ] ) );
		const changes = patch[CHANGES];
		return patch[GIDS].map( gid => {
			const old = before.get( gid );
			if ( !Object.hasOwn( changes, gid ) ) return old;
			return old ? applyDelta( old, changes[gid] ) : changes[gid];
		} );
	}
	if ( plainObject( patch ) && Object.hasOwn( patch, LENGTH ) ) {
		const base = Array.isArray( previous ) ? previous : [];
		const result = base.slice( 0, patch[LENGTH] );
		for ( const [key, item] of Object.entries( patch ) ) {
			if ( key === LENGTH ) continue;
			const index = Number( key );
			result[index] = index < base.length ? applyDelta( base[index], item ) : item;
		}
		return result;
	}
	if ( plainObject( patch ) && Object.hasOwn( patch, DELTA ) ) {
		const base = plainObject( previous ) ? previous : {}, changes = patch[DELTA];
		const removed = new Set( changes[REMOVED] ?? [] ), result = {};
		for ( const key of Object.keys( base ) ) {
			if ( removed.has( key ) ) continue;
			result[key] = Object.hasOwn( changes, key ) ? applyDelta( base[key], changes[key] ) : base[key];
		}
		for ( const [key, item] of Object.entries( changes ) ) {
			if ( key !== REMOVED && !Object.hasOwn( base, key ) ) result[key] = item;
		}
		return result;
	}
	return patch;
}
