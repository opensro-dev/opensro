/*
===========================================================================

window-positions.ts - where the remembered windows open next session

The v1.150 client keeps ten window positions in Setting\wndpos.dat
(CGInterface_SaveWindowPositions 6A01B0): version 3, a count of 10, the
screen width and height, then x,y for each window in a fixed order. The
file is written at logout and restart, and read when the interface is
created (CGInterface_LoadWindowPositions 6A06B0), which applies it only
when the saved width and height equal the active video mode.

The extended quickslot (0x85) is the tenth window; the port keeps its
position with the rest of its options (extended-quickslot.ts), so it is
not repeated here. Browser storage belongs to Platform.

===========================================================================
*/

export type RememberedWindow =
	| "mainPopup"
	| "store"
	| "storageRoom"
	| "exchange"
	| "worldMap"
	| "cosWindow"
	| "gameGuide"
	| "alchemyBox"
	| "autoPotion";

/*
================
rememberedWindows

The ginterface.txt windows 6A01B0 writes, in its order, by native id.
================
*/
export function rememberedWindows(): readonly { readonly key: RememberedWindow; readonly nativeId: number; }[] {
	return [
		{ key: "mainPopup", nativeId: 0x19 },
		{ key: "store", nativeId: 0x0f },
		{ key: "storageRoom", nativeId: 0x13 },
		{ key: "exchange", nativeId: 0x1a },
		{ key: "worldMap", nativeId: 0x1c },
		{ key: "cosWindow", nativeId: 0x78 },
		{ key: "gameGuide", nativeId: 0x20 },
		{ key: "alchemyBox", nativeId: 0x2c },
		{ key: "autoPotion", nativeId: 0x87 }
	];
}

export interface WindowPositions {
	readonly width: number;
	readonly height: number;
	readonly windows: Readonly<Partial<Record<RememberedWindow, readonly [number, number]>>>;
}

// A coordinate past this is not a screen position (the quickslot bound).
const MAX_WINDOW_COORDINATE = 65536;

/*
================
windowPositions

Validates a stored record; throws on anything malformed, as the other
preference readers do.
================
*/
export function windowPositions( value: unknown ): WindowPositions {
	const row = value as WindowPositions;
	if ( !row || typeof row !== "object" || !validExtent( row.width ) || !validExtent( row.height ) ) {
		throw Error( "Invalid window positions" );
	}
	if ( !row.windows || typeof row.windows !== "object" || Array.isArray( row.windows ) ) {
		throw Error( "Invalid window positions" );
	}
	const windows: Partial<Record<RememberedWindow, readonly [number, number]>> = {}, known = rememberedWindows();
	for ( const [key, position] of Object.entries( row.windows ) ) {
		if ( !known.some( w => w.key === key ) || !validPosition( position ) ) {
			throw Error( "Invalid window positions" );
		}
		windows[key as RememberedWindow] = [ position[0], position[1] ];
	}
	return { width: row.width, height: row.height, windows };
}

/*
================
positionsForViewport

6A06B0 applies the file only when its width and height equal the active
mode; any other size opens every window at its default.
================
*/
export function positionsForViewport(
	saved: WindowPositions | null,
	width: number,
	height: number
): WindowPositions["windows"] {
	if ( !saved || saved.width !== width || saved.height !== height ) return {};
	return saved.windows;
}

/*
================
validExtent
================
*/
function validExtent( n: unknown ): n is number {
	return Number.isInteger( n ) && (n as number) > 0 && (n as number) <= MAX_WINDOW_COORDINATE;
}

/*
================
validPosition
================
*/
function validPosition( p: unknown ): p is readonly [number, number] {
	return Array.isArray( p ) && p.length === 2 &&
		p.every( n => Number.isFinite( n ) && Math.abs( n ) <= MAX_WINDOW_COORDINATE );
}
