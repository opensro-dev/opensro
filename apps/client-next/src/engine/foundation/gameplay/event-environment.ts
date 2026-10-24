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
