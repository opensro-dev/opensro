/*
===========================================================================

speech.ts - the speech boards over talking players

sub_856680 replaces a speaker's board text and rearms timer 10 for 10,000 ms
when a chat line arrives. Lines are admitted by sequence once; the chat
history the client keeps across a teleport or world entry is not speech
being said again.

===========================================================================
*/
import type { ChatLine } from "@/engine/contracts/gameplay";

// sub_856680 timer 10.
const SPEECH_MS = 10000;
// Channels whose lines show over the speaker: all, party, guild, union.
const SPEECH_CHANNELS = [ 1, 3, 6, 13 ];

/*
================
createSpeech
================
*/
export function createSpeech() {
	// The newest sequence already admitted; undefined right after a reset,
	// when the next step adopts the existing history as already said.
	let observed: number | undefined = 0;
	const active = new Map<number, { text: string; channel: number; expires: number; }>();
	return {
		/*
================
step

Admit lines newer than the last admitted sequence and retire boards that
expired or whose speaker left view.
================
		*/
		step( lines: readonly ChatLine[], players: readonly { gid: number; name?: string; }[], now: number ) {
			const present = new Set( players.map( p => p.gid ) );
			if ( observed === undefined ) {
				observed = 0;
				for ( const line of lines ) {
					if ( line.sequence !== undefined ) observed = Math.max( observed, line.sequence );
				}
			}
			for ( const line of lines ) {
				if ( line.sequence === undefined || line.sequence <= observed ) continue;
				observed = line.sequence;
				if ( !SPEECH_CHANNELS.includes( line.channel ) ) continue;
				const gid = line.gid ?? players.find( p => p.name === line.name )?.gid;
				if ( gid !== undefined && present.has( gid ) ) {
					active.set( gid, { text: line.text, channel: line.channel, expires: now + SPEECH_MS } );
				}
			}
			for ( const [gid, row] of active ) if ( now >= row.expires || !present.has( gid ) ) active.delete( gid );
			return active;
		},
		/*
================
deadline
================
		*/
		deadline() {
			let at = Infinity;
			for ( const row of active.values() ) at = Math.min( at, row.expires );
			return at;
		},
		/*
================
reset

Leaving the world clears the boards. The retained history is rebaselined,
not replayed: a teleport or re-entry must not re-say old lines.
================
		*/
		reset() {
			observed = undefined;
			active.clear();
		}
	};
}
