import { decodeWeather, type WeatherOptions } from "./weather";
export interface EventEnvironment {
	readonly groups: Readonly<Record<number, number>>;
	readonly server: WeatherOptions;
	readonly debug: WeatherOptions | null;
	readonly effective: WeatherOptions;
}
// 77B220 entry, 761430 (3347), 7775A0 (3BDE). 7E2980/7E29E0
// look up the stack-supplied group key; they do not access the map head.
// The event weather is a derivation, not a stored write: group 1's bit 0
// stands in for the (3, 20) a packet lands as while the group is active.
function derivedEffective(
	groups: Readonly<Record<number, number>>,
	server: WeatherOptions,
	debug: WeatherOptions | null
): WeatherOptions {
	return debug ?? (((groups[1] ?? 0) & 1) !== 0 ? { mode: 3, amount: 20 } : server);
}
export function entryEnvironment( value: unknown ): { state: EventEnvironment; notices: readonly string[]; } {
	const ids = (value as { character?: { enterEventGroupIds?: unknown; }; })?.character?.enterEventGroupIds ?? [];
	if (
		!Array.isArray( ids ) || ids.length > 255 ||
		ids.some( id => !Number.isInteger( id ) || id < 0 || id > 0xffffffff )
	) throw Error( "Invalid entry event groups" );
	const groups: Record<number, number> = {}, notices: string[] = [];
	for ( const id of ids ) {
		groups[id] = 1;
		if ( id === 1 ) notices.push( "UIIT_MSG_EVENT_START" );
	}
	const server: WeatherOptions = { mode: 1, amount: 0 };
	return { state: { groups, server, debug: null, effective: derivedEffective( groups, server, null ) }, notices };
}
export function environmentPacket(
	state: EventEnvironment,
	opcode: number,
	p: Uint8Array
): { state: EventEnvironment; notice?: string; } | null {
	// 7775A0 opens with the console's hold byte: while a console rain or snow
	// stands, the packet is discarded whole and neither slot moves.
	if ( opcode === 0x3bde ) {
		if ( state.debug ) return { state };
		const server = decodeWeather( p );
		return { state: { ...state, server, effective: derivedEffective( state.groups, server, null ) } };
	}
	if ( opcode !== 0x3347 ) return null;
	if ( p.length !== 5 ) throw Error( "Invalid event state packet" );
	const id = new DataView( p.buffer, p.byteOffset, p.byteLength ).getUint32( 0, true ), active = p[4]!;
	const next = { ...state, groups: { ...state.groups, [id]: active } };
	// Only exact 0/1 changes weather immediately. Subsequent 3BDE checks bit 0.
	if ( id !== 1 || (active !== 0 && active !== 1) ) return { state: next };
	return {
		state: { ...next, effective: derivedEffective( next.groups, next.server, next.debug ) },
		notice: active === 1 ? "UIIT_MSG_EVENT_START" : "UIIT_MSG_EVENT_END"
	};
}
/*
================
transferEnvironment

A world reset (0x3369, 0x366A) keeps the event groups and drops the
server's weather and the console's hold. The browser's entry blob comes
before the 0x3369 that opens its own packet sequence, so a reset that
dropped the groups would erase what the entry just set (the Christmas
snow and the seasonal decoration were gone on every first login and back
after a teleport, whose reset comes before its entry). Every entry
replaces the groups whole and 0x3347 changes one, so keeping them across
the reset never outlives the server's word.
================
*/
export function transferEnvironment( state: EventEnvironment ): EventEnvironment {
	const server: WeatherOptions = { mode: 1, amount: 0 };
	return { groups: state.groups, server, debug: null, effective: derivedEffective( state.groups, server, null ) };
}

// The debug console's weather rows (690C40 ids 99/105-107). Rain and snow
// write the console's own slot and hold it: 7775A0 discards every 3BDE
// while the hold stands. Clear releases the hold, writing its (1, amount)
// into the server slot like the native clear writes the live weather.
// Power (no mode) only tunes the live slot's amount and never sets or
// clears the hold: under a hold the live slot is the held one.
export function debugWeather( state: EventEnvironment, mode: 1 | 2 | 3 | undefined, amount: number ): EventEnvironment {
	const value = Math.min( 255, Math.max( 0, Math.trunc( amount ) ) );
	if ( mode === 1 ) {
		const server: WeatherOptions = { mode: 1, amount: value };
		return { ...state, server, debug: null, effective: derivedEffective( state.groups, server, null ) };
	}
	if ( mode !== undefined ) {
		const debug: WeatherOptions = { mode, amount: value };
		return { ...state, debug, effective: debug };
	}
	if ( state.debug ) {
		const debug: WeatherOptions = { mode: state.debug.mode, amount: value };
		return { ...state, debug, effective: debug };
	}
	const server: WeatherOptions = { mode: state.server.mode, amount: value };
	return { ...state, server, effective: derivedEffective( state.groups, server, null ) };
}

// CHRISTMAS_EVENT_GROUP is the native Christmas event's group (the
// stolen socks): its notice and its snow. CAROL_MUSIC_MODE is the music
// manager's carol mode (8F7A43, event_carol_0%d.ogg).
const CHRISTMAS_EVENT_GROUP = 1;
const CAROL_MUSIC_MODE = 1;
// TOWN_CAROLS_EVENT_GROUP is port-only, not native: the server's switch
// for So-Ok's Christmas Gift's carols in the towns, the whole window
// (action/event_groups.go ChristmasTownCarolsEventGroup). No native table
// names it. TOWN_CAROL_MUSIC_MODE is port-only too: no native music mode
// is 4; regionMusicMode resolves it where the region is known.
const TOWN_CAROLS_EVENT_GROUP = 2006;
export const TOWN_CAROL_MUSIC_MODE = 4;
// CAROL_TOWN_PROFILES are the ambient profiles (effectenvsnd.txt) of the
// towns whose squares play the carols: Jangan, Donwhang, Hotan,
// Samarkand, Constantinople and Alexandria (the later client's profile,
// present when the shard carries it). The fortress and field profiles
// beside them keep their own music.
const CAROL_TOWN_PROFILES: readonly string[] = [
	"장안",
	"돈황",
	"호탄",
	"사마르칸트",
	"콘스탄티노플",
	"알렉산드리아"
] as const;

/*
================
eventMusicMode

Port-only, not native (the Experimental "Christmas carols" row): the
carol mode while the Christmas event group is on and no other special
music (the fortress war's) holds the latch. No v1.150 code sets the
native carol mode, so carols off is the native region music. While the
town carols' group is on (So-Ok's Christmas Gift) the town carol mode
wins over the snow's: the carols play in the towns whether it snows or
not, and nowhere else.
================
*/
export function eventMusicMode( mode: number, groups: Readonly<Record<number, number>>, carols: boolean ): number {
	if ( !carols || mode !== 0 ) return mode;
	if ( ((groups[TOWN_CAROLS_EVENT_GROUP] ?? 0) & 1) !== 0 ) return TOWN_CAROL_MUSIC_MODE;
	return ((groups[CHRISTMAS_EVENT_GROUP] ?? 0) & 1) !== 0 ? CAROL_MUSIC_MODE : mode;
}

/*
================
regionMusicMode

The music mode the region's track plays under: the town carol mode is
the carol mode inside a carol town's profile and the region's own track
everywhere else, so leaving the town brings its music back. Every other
mode passes through.
================
*/
export function regionMusicMode( mode: number, profile: string | null | undefined ): number {
	if ( mode !== TOWN_CAROL_MUSIC_MODE ) return mode;
	return profile && CAROL_TOWN_PROFILES.includes( profile ) ? CAROL_MUSIC_MODE : 0;
}
