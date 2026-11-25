/*
===========================================================================

peer-guild-face.ts - a nearby player's guild nameplate, re-sent live

The spawn row carries a player's guild once. When that player creates,
joins, leaves or loses a guild, or gets a new grant name, the server
re-sends the guild face to everyone who already sees them (#4413):

  0x365F CPSMission_OnPeerGuildAppearance 75FE90
    u32 gid, u32 guildId, ascii name; when the name is not empty:
    ascii grant, u32 crest, u32 alliance id, u32 alliance crest,
    u8 fortress role (CICPlayer_BindGuild 869810)
  0x37E6 CPSMission_OnPeerGuildCleared 75C890
    u32 gid (CICPlayer_UnbindGuild 868800)

Both are registered in CPSMission_RegisterPacketHandlers (74E833,
74E854). The fields are the spawn row's guild sub-block
(peer-appearance.ts), so the entity keeps the same names.

===========================================================================
*/
import { viewOf } from "@/engine/foundation/binary/byte-view";

/*
================
PeerGuildFace

The entity fields a face frame writes: the spawn decoder's names. An
empty name is a guildless nameplate.
================
*/
export interface PeerGuildFace {
	readonly gid: number;
	readonly guildName: string;
	readonly guildId: number | undefined;
	readonly guildGrantName: string | undefined;
	readonly guildCrests: readonly [number, number, number] | undefined;
	readonly guildWarTeam: number | undefined;
}

/*
================
decodePeerGuildAppearance

Strict: a short or trailing-byte body throws, as the spawn decoder does.
The ascii strings map byte for byte, like the spawn row's str(). An empty
name is null: 75FE90 binds nothing then (its empty arm only touches the
guild window's member tab), so the nameplate stays as it was. The server
sends 0x37E6 for a guildless player instead.
================
*/
export function decodePeerGuildAppearance( p: Uint8Array ): PeerGuildFace | null {
	const v = viewOf( p );
	let o = 0;
	const take = ( n: number ) => {
		if ( o + n > p.length ) throw new Error( "Invalid peer guild face" );
		const at = o;
		o += n;
		return at;
	};
	const u8 = () => p[take( 1 )] as number;
	const u32 = () => v.getUint32( take( 4 ), true );
	const str = () => {
		const n = v.getUint16( take( 2 ), true ), at = take( n );
		return Array.from( p.subarray( at, at + n ), c => String.fromCharCode( c ) ).join( "" );
	};
	const gid = u32(), guildId = u32(), guildName = str();
	if ( !guildName ) {
		if ( o !== p.length ) throw new Error( "Invalid peer guild face" );
		return null;
	}
	const guildGrantName = str();
	const guildCrests = [ u32(), u32(), u32() ] as const;
	const guildWarTeam = u8();
	if ( o !== p.length ) throw new Error( "Invalid peer guild face" );
	return { gid, guildName, guildId, guildGrantName, guildCrests, guildWarTeam };
}

/*
================
decodePeerGuildCleared
================
*/
export function decodePeerGuildCleared( p: Uint8Array ): PeerGuildFace {
	if ( p.length !== 4 ) throw new Error( "Invalid peer guild clear" );
	return {
		gid: viewOf( p ).getUint32( 0, true ),
		guildName: "",
		guildId: undefined,
		guildGrantName: undefined,
		guildCrests: undefined,
		guildWarTeam: undefined
	};
}
