/*
===========================================================================

debug-commands.ts - the client's config\command.txt developer commands

CGInterface_LoadCommandBindings (68D9C0) reads `<id> : "/name fmt"` rows
into a set keyed by the quoted text's first word. The debug console
(Console_OnInputKey -> ChatInput_HandleCheatCommand 50F260) tries a GM
command first and, when none matched, splits the line on tab, CR and
space (CGInterface_ParseSlashCommand 68DD10), looks the first word up
and hands the id and the remaining words to CGInterface_OnCommandMessage
(690C40). The console itself opens only for a test GM
(Console_Toggle 4FA710, Client_IsDebugCommandAllowed 4FA670); ordinary
chat parses the table only under g_bEnableFrameStats, which retail never
sets, and then swallows the line without running it.

===========================================================================
*/

// The ids the console family handles (690C40 through its 691CC4 table).
export const DEBUG_COMMAND_PLAYER_COUNT = 0;
export const DEBUG_COMMAND_DEBUG = 1;
export const DEBUG_COMMAND_MESSAGE_CLEAR = 2;
export const DEBUG_COMMAND_POSITION = 3;
export const DEBUG_COMMAND_GET_POSITION = 4;
export const DEBUG_COMMAND_NULL = 5;
// 690C40's 0x190 branch: use the bag slot named by the single argument.
export const DEBUG_COMMAND_ITEM = 400;
// The weather and time families (690C40 ids 99-109): the interface's own
// weather and timer fields, driven locally with no server frame.
export const DEBUG_COMMAND_POWER = 99;
export const DEBUG_COMMAND_TIME_OFF = 100;
export const DEBUG_COMMAND_TIME_ON = 101;
export const DEBUG_COMMAND_TIME_SET = 102;
export const DEBUG_COMMAND_DAY = 103;
export const DEBUG_COMMAND_NIGHT = 104;
export const DEBUG_COMMAND_RAIN = 105;
export const DEBUG_COMMAND_SNOW = 106;
export const DEBUG_COMMAND_CLEAR = 107;
export const DEBUG_COMMAND_TIME_PLUS = 108;
export const DEBUG_COMMAND_TIME_MINUS = 109;
// The camera family (690C40 ids 110-135): the interface's zoom byte
// (+0x3CF), its free-camera byte (+0x3CE), and the cursor manager's
// visibility byte.
export const DEBUG_COMMAND_ZOOM = 110;
// 111 snaps the interface's second camera float (+0x7C0, seeded 50)
// between its 50 and 240 presets against the double 50. Nothing in the
// binary reads the field; the snap is kept as the handler leaves it.
export const DEBUG_COMMAND_FAST = 111;
// 112/113 write the local player's shown byte (CICPlayer +0xB8): while
// it stands the character's own animation advance submits its body
// (853600); cleared, the whole local presentation is skipped. 115/116
// write the mission window's visible flag (CIFWnd +0x6D through
// CIFWnd_SetVisibleFlag 9FCD70): the HUD tree shows or hides, and
// hiding releases the hovered window.
export const DEBUG_COMMAND_SHOW_ME = 112;
export const DEBUG_COMMAND_HIDE_ME = 113;
export const DEBUG_COMMAND_FREE_CAMERA = 114;
export const DEBUG_COMMAND_UI_OFF = 115;
export const DEBUG_COMMAND_UI_ON = 116;
// 117 flips the engine global's frame byte (+0x4A), the switch the
// native frame-statistics readout reads; a GM string arm flips the
// same byte. The port keeps the byte (see #1717 for the readout).
export const DEBUG_COMMAND_FRAME = 117;
// 118/119 write the interface's camera-collision byte (+0x3CC), which
// the constructor sets: ApplyCameraOrbit (68FAD0) is the only
// consumer - set, it casts the eye-to-target segment and pulls the
// camera in on a hit; clear, it takes the same no-hit arm as a miss
// and the camera keeps the requested distance. The follow
// (UpdateCameraOrbitTarget 68F830) never reads the byte, so the
// camera keeps tracking the player either way. "col" is collision.
export const DEBUG_COMMAND_COLON = 118;
export const DEBUG_COMMAND_COLON_OFF = 119;
// The camera-waypoint rows (690C40 ids 130-134, #1716): the interface's
// keyframe track at +0x3FC and its play byte (+0x3CD). 130 resets the
// track; 131 and 132 are one shared case (the free-camera byte standing
// and a non-empty store, then the play byte is written and the track
// clock zeroed); 133 clears the play byte; 134 writes the store's lines
// in the native text form. The store itself lives in camera-waypoints.ts.
export const DEBUG_COMMAND_WAYPOINT_CLEAR = 130;
export const DEBUG_COMMAND_WAYPOINT_POP = 131;
export const DEBUG_COMMAND_WAYPOINT_GO = 132;
export const DEBUG_COMMAND_WAYPOINT_STOP = 133;
export const DEBUG_COMMAND_WAYPOINT_SAVE = 134;
export const DEBUG_COMMAND_CURSOR = 135;
// 136 writes the interface's free-camera move speed (+0x7B0, seeded
// 100) from one word clamped to 1..500. 137 flips the global /id byte
// (0xCCB5D8). Its readers draw while it stands: the name board and the
// kind-4 overhead pass (85F5DC) and a hit's damage number (8D5856), so
// clearing it hides them; it adds no line (#1785). The byte ships
// standing (its file-backed initial is 1 and the case is the only
// writer in the image), so the kept copy seeds standing too. The
// port's camera has no free translation; that value is kept as
// written (see #1717).
export const DEBUG_COMMAND_CAMERA_SPEED = 136;
export const DEBUG_COMMAND_ID = 137;
// The render family (690C40 ids 150-155, handlers 691686-6917A3): each
// negates one SWorld graphic option (+0x2E50 + 4*mode, seeded by
// SWorld_Construct) through SWorld_ApplyGraphicOption (8A54C0). The
// option numbers live in foundation/rendering/graphic-options.ts.
export const DEBUG_COMMAND_WIRE_FRAME = 150;
export const DEBUG_COMMAND_GROUND = 151;
export const DEBUG_COMMAND_SKY = 152;
export const DEBUG_COMMAND_MAP_OBJECTS = 153;
export const DEBUG_COMMAND_CHARACTERS = 154;
export const DEBUG_COMMAND_LEVEL_OF_DETAIL = 155;
export const DEBUG_COMMAND_SET_FIELD_OF_VIEW = 500;
// The over-400 family's high rows (690C40's second table): 600 writes
// the local player's (or its mount's) speed field (+0x658), 601 builds
// the named item's descriptor, frames it as a packet stream headed by
// opcode 0x3314 and hands it to the client's own packet dispatch
// (690BD0) - nothing leaves the machine - 602 has no case at all (the
// table sends it to the shared exit) and 603 hands a sound name to the
// sound manager, which the audio owner now answers by name. The port's
// movement and inventory belong to the server; the 600/601 handlers
// run and the values are kept (see #1717).
export const DEBUG_COMMAND_SET_SPEED = 600;
export const DEBUG_COMMAND_EQUIP = 601;
export const DEBUG_COMMAND_MAKE_OBJECT = 602;
export const DEBUG_COMMAND_SOUND = 603;

const MIN_FIELD_OF_VIEW_DEGREES = 10;
const MAX_FIELD_OF_VIEW_DEGREES = 150;
// f658 client CRT decimal helper 9E168B: ten digits at each BMP start.
const NATIVE_DECIMAL_DIGIT_STARTS = [
	0x0030,
	0x0660,
	0x06f0,
	0x0966,
	0x09e6,
	0x0a66,
	0x0ae6,
	0x0b66,
	0x0c66,
	0x0ce6,
	0x0d66,
	0x0e50,
	0x0ed0,
	0x0f20,
	0x1040,
	0x17e0,
	0x1810,
	0xff10
] as const;
const DECIMAL_RADIX = 10;
const NATIVE_BYTE_MAX = 0xff;

/*
================
DebugCommand

One parsed console line: its table id and the words after the name.
================
*/
export interface DebugCommand {
	readonly id: number;
	readonly args: readonly string[];
}

/*
================
parseCommandTable

68D9C0: each non-empty line not starting with `//` is an integer id, a
colon and a quoted pattern; the pattern's first word (up to a space or the
closing quote) is the key. A repeated key is the asserted error at 68DB4C.
================
*/
export function parseCommandTable( text: string ): ReadonlyMap<string, number> {
	const table = new Map<string, number>();
	for ( const raw of text.split( /\r?\n/ ) ) {
		const line = raw.trim();
		if ( !line || line.startsWith( "//" ) ) continue;
		const match = /^(-?\d+)\s*:\s*"([^"]*)"/.exec( line );
		if ( !match ) throw Error( "Invalid command table row: " + line );
		const name = match[2]!.split( " " )[0]!;
		if ( !name || table.has( name ) ) throw Error( "Invalid command table key: " + name );
		table.set( name, Number( match[1] ) );
	}
	return table;
}

/*
================
decodeCommandTable

68D9C0 converts the file with MultiByteToWideChar(CP_ACP): the shipped
command.txt is CP949 (Korean comment lines), which WHATWG names euc-kr.
================
*/
export function decodeCommandTable( bytes: ArrayBuffer ): ReadonlyMap<string, number> {
	return parseCommandTable( new TextDecoder( "euc-kr", { fatal: true } ).decode( new Uint8Array( bytes ) ) );
}

/*
================
commandInteger

j_Fn_Wraps_CRT_wcstol: the leading decimal digits (with an optional sign),
zero when there are none.
================
*/
export function commandInteger( word: string ): number {
	const match = /^\s*[+-]?\d+/.exec( word );
	return match ? Number.parseInt( match[0], 10 ) : 0;
}

/*
================
commandFloat

atof as the time family reads its hour: the leading decimal number, zero
when there is none.
================
*/
export function commandFloat( word: string ): number {
	const value = Number.parseFloat( word );
	return Number.isFinite( value ) ? value : 0;
}

/*
================
commandSpace

The initial CRT table at C32A3A has mask 8 only for these byte characters.
For larger code units, 9D259B delegates to Windows GetStringTypeW(C1_SPACE).
Inference: Unicode White_Space approximates that OS-dependent result across
platforms; the proven byte table takes precedence over Unicode properties.
================
*/
function commandSpace( code: number ): boolean {
	if ( code <= NATIVE_BYTE_MAX ) return code >= 0x09 && code <= 0x0d || code === 0x20 || code === 0xa0;
	return /^\p{White_Space}$/u.test( String.fromCharCode( code ) );
}

/*
================
commandDecimalDigit
================
*/
function commandDecimalDigit( code: number ): number {
	for ( const first of NATIVE_DECIMAL_DIGIT_STARTS ) {
		const digit = code - first;
		if ( digit >= 0 && digit < DECIMAL_RADIX ) return digit;
	}
	return -1;
}

/*
================
nativeWordInteger

9C3720's digit walk, shared by the bounded integer commands: the
leading decimal digits (the ten BMP digit blocks commandDecimalDigit
knows) after an optional sign, with the magnitude bounded at the
caller's cap so a long word saturates the way the CRT conversion does
before the handler's own clamp sees it.
================
*/
function nativeWordInteger( word: string, magnitudeCap: number ): { negative: boolean; magnitude: number; } {
	let index = 0;
	while ( index < word.length && commandSpace( word.charCodeAt( index ) ) ) index++;
	const negative = word[index] === "-";
	if ( negative || word[index] === "+" ) index++;
	let magnitude = 0;
	for ( ; index < word.length; index++ ) {
		const digit = commandDecimalDigit( word.charCodeAt( index ) );
		if ( digit < 0 ) break;
		magnitude = Math.min( magnitudeCap, magnitude * DECIMAL_RADIX + digit );
	}
	return { negative, magnitude };
}

/*
================
debugFieldOfView

Command 500 accepts exactly one word. 9C3720 reads its signed decimal prefix
with signed 32-bit saturation; the handler then clamps the unsigned result.
Bounding the magnitude at 151 preserves that result even on CRT overflow:
every nonzero negative and every positive value above 150 becomes 150.
================
*/
export function debugFieldOfView( args: readonly string[] ): number | null {
	if ( args.length !== 1 ) return null;
	const word = nativeWordInteger( args[0]!, MAX_FIELD_OF_VIEW_DEGREES + 1 );
	if ( word.negative && word.magnitude !== 0 ) return MAX_FIELD_OF_VIEW_DEGREES;
	return Math.max( MIN_FIELD_OF_VIEW_DEGREES, Math.min( MAX_FIELD_OF_VIEW_DEGREES, word.magnitude ) );
}

/*
================
debugPositionHex

The /Pos region token: swscanf "%x" - hex digits stop at the first
non-hex character, no digits (or a missing token) reads 0.
================
*/
function debugPositionHex( token: string | undefined ): number {
	if ( !token ) return 0;
	const value = Number.parseInt( token, 16 );
	return Number.isNaN( value ) ? 0 : value;
}

/*
================
debugPositionFloat

One /Pos coordinate token: CRT_atof - a leading float, no digits (or a
missing token) reads 0.
================
*/
function debugPositionFloat( token: string | undefined ): number {
	if ( !token ) return 0;
	const value = Number.parseFloat( token );
	return Number.isNaN( value ) ? 0 : value;
}

/*
================
debugPosition

690C40 case 2 (/Pos %s): the single argument word is comma-split into
four tokens - a hex region, then x, y, z floats - and handed to
CNavigationController_SetTargetDestination (86D5C0) on the local player
(or its mount vehicle), which is the ground-click walk destination, not
a teleport. The native handler runs only when an argument word exists
(CGInterface_Helper4BDEF0 reports the word count) and reads the first
word alone. The region lands in the destination's u16 field (86D5C0
reads it at +8), so it is masked to 16 bits here.
================
*/
export function debugPosition(
	args: readonly string[]
): { regionId: number; x: number; y: number; z: number; } | null {
	if ( args.length < 1 ) return null;
	const tokens = args[0]!.split( "," );
	return {
		regionId: debugPositionHex( tokens[0] ) & 0xffff,
		x: debugPositionFloat( tokens[1] ),
		y: debugPositionFloat( tokens[2] ),
		z: debugPositionFloat( tokens[3] )
	};
}

/*
================
debugCommand

68DD10: a line starting with '/' whose first word names a table row. The
lookup is the set's exact (case-sensitive) key, as the native map compares.
================
*/
export function debugCommand( table: ReadonlyMap<string, number>, line: string ): DebugCommand | null {
	if ( !line.startsWith( "/" ) ) return null;
	const words = line.split( /[\t\r ]+/ ).filter( Boolean );
	const id = table.get( words[0] ?? "" );
	return id === undefined ? null : { id, args: words.slice( 1 ) };
}

const MIN_CAMERA_SPEED = 1;
const MAX_CAMERA_SPEED = 500;
const MIN_SET_SPEED = 10;
const MAX_SET_SPEED = 500;
const SET_SPEED_WORD_SCALE = 10;
const FAST_SPEED_LOW = 50;
const FAST_SPEED_HIGH = 240;

/*
================
debugCameraSpeedValue

690C40 case 136 (/cs %d): exactly one word, read by 9C3720 and clamped
to 1..500 with the handler's unsigned compares, so a negative word
lands on 500. 68EF8B seeds the field it writes 100.
================
*/
export function debugCameraSpeedValue( args: readonly string[] ): number | null {
	if ( args.length !== 1 ) return null;
	const word = nativeWordInteger( args[0]!, MAX_CAMERA_SPEED + 1 );
	if ( word.negative && word.magnitude !== 0 ) return MAX_CAMERA_SPEED;
	return Math.max( MIN_CAMERA_SPEED, Math.min( MAX_CAMERA_SPEED, word.magnitude ) );
}

/*
================
debugSetSpeedValue

690C40 case 600 (/setspeed %d): the first word alone (a missing word
reads as zero), read by 9C3720, multiplied by ten and clamped to
10..500 with the handler's unsigned compares, so a negative word
lands on 500. The result is the value the handler stores as a float
into the local player's speed field (+0x658), or its mount's when a
mount is under it (85D870 picks the target).
================
*/
export function debugSetSpeedValue( args: readonly string[] ): number {
	const word = nativeWordInteger( args[0] ?? "", MAX_SET_SPEED + 1 );
	if ( word.negative && word.magnitude !== 0 ) return MAX_SET_SPEED;
	return Math.max( MIN_SET_SPEED, Math.min( MAX_SET_SPEED, word.magnitude * SET_SPEED_WORD_SCALE ) );
}

/*
================
debugFastSpeedValue

690C40 case 111 (/fast): the interface float (+0x7C0, seeded 50)
against the double 50 - above it the handler writes 50, at or below
it writes 240.
================
*/
export function debugFastSpeedValue( current: number ): number {
	return current > FAST_SPEED_LOW ? FAST_SPEED_LOW : FAST_SPEED_HIGH;
}
