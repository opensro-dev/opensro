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
export const DEBUG_COMMAND_NULL = 5;
// 690C40's 0x190 branch: use the bag slot named by the single argument.
export const DEBUG_COMMAND_ITEM = 400;
export const DEBUG_COMMAND_SET_FIELD_OF_VIEW = 500;

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
debugFieldOfView

Command 500 accepts exactly one word. 9C3720 reads its signed decimal prefix
with signed 32-bit saturation; the handler then clamps the unsigned result.
Bounding the magnitude at 151 preserves that result even on CRT overflow:
every nonzero negative and every positive value above 150 becomes 150.
================
*/
export function debugFieldOfView( args: readonly string[] ): number | null {
	if ( args.length !== 1 ) return null;
	const word = args[0]!;
	let index = 0;
	while ( index < word.length && commandSpace( word.charCodeAt( index ) ) ) index++;
	const negative = word[index] === "-";
	if ( negative || word[index] === "+" ) index++;
	let magnitude = 0;
	for ( ; index < word.length; index++ ) {
		const digit = commandDecimalDigit( word.charCodeAt( index ) );
		if ( digit < 0 ) break;
		magnitude = Math.min( MAX_FIELD_OF_VIEW_DEGREES + 1, magnitude * DECIMAL_RADIX + digit );
	}
	if ( negative && magnitude !== 0 ) return MAX_FIELD_OF_VIEW_DEGREES;
	return Math.max( MIN_FIELD_OF_VIEW_DEGREES, Math.min( MAX_FIELD_OF_VIEW_DEGREES, magnitude ) );
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
