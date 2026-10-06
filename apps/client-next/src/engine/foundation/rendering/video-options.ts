/*
===========================================================================

video-options.ts - the Option window's video records and screen size

Two native detail records (5CDC20 defaults) plus the screen size: like the
native client's window mode, the game area is the chosen width x height,
centred on a full-screen page at one UI pixel per CSS pixel (platform
displayScale). It shrinks only when the page is smaller than the mode.

===========================================================================
*/
// User-selected compatibility mode while whole-scene lighting parity is open.
// true restores actor ambient .6 and temporary hit lighting.
// Metal Detail independently controls the existing sphere-reflection pass.
export const NATIVE_CHARACTER_LIGHTING = false;
// Zero follows requestAnimationFrame at the current display refresh rate.
export const DEFAULT_FRAME_LIMIT = 0;
/*
================
frameLimits

Browser presentation preference; never encoded into the native detail banks.
================
*/
export function frameLimits(): readonly number[] {
	return [ 60, 120, 240, 0 ];
}
/*
================
VideoRecords
================
*/
type VideoRecords = readonly [readonly number[], readonly number[]];
// Screen size (Option window GDR_OPT_VIDEO_CB_SS). The first entry is the
// whole page; the others are fixed modes drawn 1:1, centred and letterboxed.
/*
================
displaySizes
================
*/
export function displaySizes(): readonly (readonly [number, number])[] {
	return [
		[ 0, 0 ],
		[ 1920, 1080 ],
		[ 1680, 1050 ],
		[ 1600, 900 ],
		[ 1440, 900 ],
		[ 1366, 768 ],
		[ 1280, 1024 ],
		[ 1280, 800 ],
		[ 1280, 720 ],
		[ 1024, 768 ],
		[ 800, 600 ]
	];
}

/*
================
displaySizeIndex

The listed mode equal to `size`, 0 (the whole page) when none is.
================
*/
export function displaySizeIndex( size: readonly [number, number] | undefined ): number {
	return size ? Math.max( 0, displaySizes().findIndex( m => m[0] === size[0] && m[1] === size[1] ) ) : 0;
}
/*
================
VideoOptions
================
*/
export interface VideoOptions {
	readonly frameLimit?: number;
	readonly custom?: VideoRecords;
	readonly active: 0 | 1;
	readonly records: readonly [readonly number[], readonly number[]];
	readonly displaySize?: readonly [number, number];
}
// 5CDC20 initializes the native medium detail record; slot 14 is not displayed.
// Slot 1 (UIIT_STT_SHADOW_DETAIL) defaults to 0 (UIIT_STT_NONE / nothing) for new players.
/*
================
defaultVideoOptions
================
*/
export function defaultVideoOptions(): VideoOptions {
	const row = [ 1, 0, 2, 2, 0, 1, 1, 1, 1, 2, 1, 0, 1, 2, 0, 0 ];
	return { active: 0, records: [ [ ...row ], [ ...row ] ], frameLimit: DEFAULT_FRAME_LIMIT };
}
// Slots 3 and 5 are selectable but change nothing, as in the native client:
// 713EC0 maps them to SWorld properties 9 and 7, which 8A54C0 only stores
// (+0x2E50 table) before posting 0x809, a message no dispatcher handles.
// Slot 4 (property 6) builds the water DuDv texture and a 512x512
// reflection target, and slot 12 (property 0xE) turns on cloth geometry
// (A2E6E0 -> manager +0x19C); neither exists in this renderer yet.
/*
================
videoRows
================
*/
export function videoRows(): readonly { slot: number; key: string; entries: readonly string[]; supported: boolean; }[] {
	return [
		{
			"slot": 0,
			"key": "UILM_TEXT_GRAPHIC_QUALITY",
			"entries": [
				"UIIT_STT_LOW",
				"UIIT_STT_MIDDLE",
				"UIIT_STT_HIGH",
				"UIIT_STT_GRAPHIC_QUALITY_CONTROL_WAR",
				"UIIT_STT_GRAPHIC_QUALITY_CONTROL_USER"
			],
			"supported": true
		},
		{
			"slot": 1,
			"key": "UIIT_STT_SHADOW_DETAIL",
			"entries": [ "UIIT_STT_NONE", "UIIT_STT_CIRCLE", "UIIT_STT_DETAIL" ],
			"supported": true
		},
		{ "slot": 2, "key": "UIIT_STT_SCENERY_SIGHT_RANGE", "entries": [ "1", "2", "3", "4", "5" ], "supported": true },
		{ "slot": 3, "key": "UIIT_STT_CHAR_SIGHT_RANGE", "entries": [ "1", "2", "3", "4", "5" ], "supported": true },
		{
			"slot": 4,
			"key": "UIIT_STT_WATER_REFLECTION",
			"entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ],
			"supported": false
		},
		{
			"slot": 5,
			"key": "UIIT_STT_WATER_DETAIL",
			"entries": [ "UIIT_STT_LOW", "UIIT_STT_MIDDLE", "UIIT_STT_HIGH" ],
			"supported": true
		},
		{ "slot": 6, "key": "UIIT_STT_METAL_DETAIL", "entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ], "supported": true },
		{ "slot": 7, "key": "UIIT_STT_LIGHT_EFFECT", "entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ], "supported": true },
		{ "slot": 8, "key": "UIIT_STT_FILTERING", "entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ], "supported": true },
		{
			"slot": 9,
			"key": "UIIT_STT_TEXTER_DETAIL",
			"entries": [ "UIIT_STT_LOW", "UIIT_STT_MIDDLE", "UIIT_STT_HIGH" ],
			"supported": true
		},
		{ "slot": 10, "key": "UIIT_STT_LENS_FLAIR", "entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ], "supported": true },
		{ "slot": 11, "key": "UIIT_STT_BLOOM_EFFECT", "entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ], "supported": true },
		{
			"slot": 12,
			"key": "UIIT_STT_DYNAMIC_ANIMATION",
			"entries": [ "UIIT_STT_OFF", "UIIT_STT_ON" ],
			"supported": false
		},
		{
			"slot": 13,
			"key": "UIIT_STT_EFFECT_QUALITY",
			"entries": [ "UIIT_STT_EFFECT_ALL_OFF", "UIIT_STT_EFFECT_DAMEGE_ON", "UIIT_STT_EFFECT_ALL_ON" ],
			"supported": true
		},
		{
			"slot": 15,
			"key": "UIIT_STT_OPTIMIZE_A_E_EXPRESSION",
			"entries": [ "UIIT_STT_OPTIMIZE_A_E_CLOTHES", "UIIT_CTL_WARENETWORK_DETAIL_NORMAL" ],
			"supported": true
		}
	];
}
/*
================
videoOptions
================
*/
export function videoOptions( value: unknown ): VideoOptions {
	const v = value as VideoOptions;
	if ( !v || (v.active !== 0 && v.active !== 1) || !Array.isArray( v.records ) || v.records.length !== 2 ) {
		throw Error( "Invalid video options" );
	}
	if ( v.custom !== undefined && (!Array.isArray( v.custom ) || v.custom.length !== 2) ) {
		throw Error( "Invalid custom video options" );
	}
	for ( const row of [ ...v.records, ...(v.custom ?? []) ] ) {
		if (
			!Array.isArray( row ) || row.length !== 16 || row[14] !== 0 || row.some( n =>
				!Number.isInteger( n ) || n < 0
			) || videoRows().some( spec => row[spec.slot]! >= spec.entries.length )
		) throw Error( "Invalid video record" );
	}
	// A saved size must be a listed mode. The retired height-only setting
	// (displayHeight) stretched the page; it is dropped, not converted.
	if ( v.displaySize !== undefined && displaySizeIndex( v.displaySize ) === 0 ) {
		throw Error( "Invalid display size" );
	}
	if ( v.frameLimit !== undefined && !frameLimits().includes( v.frameLimit ) ) {
		throw Error( "Invalid frame limit" );
	}
	return {
		frameLimit: v.frameLimit ?? DEFAULT_FRAME_LIMIT,
		active: v.active,
		records: [ [ ...v.records[0] ], [ ...v.records[1] ] ],
		...(v.custom ? { custom: [ [ ...v.custom[0] ], [ ...v.custom[1] ] ] as VideoRecords } : {}),
		...(v.displaySize ? { displaySize: [ v.displaySize[0], v.displaySize[1] ] as const } : {})
	};
}
/*
================
changeVideo
================
*/
export function changeVideo( options: VideoOptions, slot: number, value: number ): VideoOptions {
	const spec = videoRows().find( s => s.slot === slot );
	if ( !spec?.supported || !Number.isInteger( value ) || value < 0 || value >= spec.entries.length ) return options;
	const records = options.records.map( row => [ ...row ] ) as [number[], number[]],
		custom = (options.custom ?? options.records).map( row => [ ...row ] ) as [number[], number[]],
		active = options.active;
	// 5CDF20 saves the visible custom bank before leaving Custom. Presets do not
	// overwrite it. 5CDFE0 changes only edited bytes before selecting Custom.
	if ( slot === 0 ) {
		if ( records[active][0] === 4 ) custom[active] = [ ...records[active] ];
		records[active] = value === 4 ? [ ...custom[active] ] : [ ...graphicPresets()[value]! ];
		records[active][0] = value;
	} else {
		if ( records[active][slot] === value ) return options;
		if ( records[active][0] === 4 ) custom[active] = [ ...records[active] ];
		custom[active][slot] = value;
		custom[active][0] = 4;
		records[active] = [ ...custom[active] ];
	}
	return { ...options, records, custom };
}

// 713EC0 maps slot 2 to SWorld property 8; 8A54C0 installs +0x2729.
// This is the culling frustum's far plane, not an object-size multiplier.
/*
================
backgroundDrawDistance
================
*/
export function backgroundDrawDistance( options: VideoOptions ): number {
	return [ 1500, 2500, 3500, 4500, 5500 ][options.records[options.active][2]!]!;
}

// 5CDC80 byte stores; slot 0 represents the separate native preset selector.
/*
================
graphicPresets
================
*/
function graphicPresets(): readonly (readonly number[])[] {
	return [
		[ 0, 1, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 2, 0, 0 ],
		[ 1, 1, 2, 2, 0, 1, 1, 1, 1, 2, 1, 1, 1, 2, 0, 0 ],
		[ 2, 2, 4, 4, 1, 2, 1, 1, 1, 2, 1, 1, 1, 2, 0, 0 ],
		[ 3, 0, 2, 4, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0 ]
	];
}
/*
================
resetVideoRecord
================
*/
export function resetVideoRecord( options: VideoOptions ): VideoOptions {
	const defaults = defaultVideoOptions(),
		records = options.records.map( r => [ ...r ] ) as [number[], number[]],
		custom = (options.custom ?? options.records).map( r => [ ...r ] ) as [number[], number[]];
	records[options.active] = [ ...defaults.records[options.active] ];
	custom[options.active] = [ ...defaults.records[options.active] ];
	return { ...options, records, custom, frameLimit: DEFAULT_FRAME_LIMIT };
}
