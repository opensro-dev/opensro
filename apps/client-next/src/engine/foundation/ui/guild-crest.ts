/*
===========================================================================

guild-crest.ts - native guild and union crests: names, URLs and pixels

A crest is the native 256-byte .crb payload: 16x16 palette indices,
bottom-up. The client names a guild's crest G{prefix}_{guild}_{revision}
and a union's A{prefix}_{union}_{revision} (client 0x833E42 / 0x833E75)
and fetches it from the Agent's /marks/ route, under the Agent API base.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";

// Retail silk.dat RGB palette SHA256 a15e9392b2f59d20b29227282ab7c50ccd4623d5492a832b888ee23003de75cc.
// 4B4C90 builds an 8-bit bottom-up BMP from 256 palette indices;
// 925B91 uses no color key. Transparent placeholders are not native crests.
const paletteHex =
	"000000800000008000808000000080800080008080808080c0dcc0a6caf02a3faa2a3fff2a5f002a5f552a5faa2a5fff2a7f002a7f552a7faa2a7fff2a9f002a9f552a9faa2a9fff2abf002abf552abfaa2abfff2adf002adf552adfaa2adfff2aff002aff552affaa2affff5500005500555500aa5500ff551f00551f55551faa551fff553f00553f55553faa553fff555f00555f55555faa555fff557f00557f55557faa557fff559f00559f55559faa559fff55bf0055bf5555bfaa55bfff55df0055df5555dfaa55dfff55ff0055ff5555ffaa55ffff7f00007f00557f00aa7f00ff7f1f007f1f557f1faa7f1fff7f3f007f3f557f3faa7f3fff7f5f007f5f557f5faa7f5fff7f7f007f7f557f7faa7f7fff7f9f007f9f557f9faa7f9fff7fbf007fbf557fbfaa7fbfff7fdf007fdf557fdfaa7fdfff7fff007fff557fffaa7fffffaa0000aa0055aa00aaaa00ffaa1f00aa1f55aa1faaaa1fffaa3f00aa3f55aa3faaaa3fffaa5f00aa5f55aa5faaaa5fffaa7f00aa7f55aa7faaaa7fffaa9f00aa9f55aa9faaaa9fffaabf00aabf55aabfaaaabfffaadf00aadf55aadfaaaadfffaaff00aaff55aaffaaaaffffd40000d40055d400aad400ffd41f00d41f55d41faad41fffd43f00d43f55d43faad43fffd45f00d45f55d45faad45fffd47f00d47f55d47faad47fffd49f00d49f55d49faad49fffd4bf00d4bf55d4bfaad4bfffd4df00d4df55d4dfaad4dfffd4ff00d4ff55d4ffaad4ffffff0055ff00aaff1f00ff1f55ff1faaff1fffff3f00ff3f55ff3faaff3fffff5f00ff5f55ff5faaff5fffff7f00ff7f55ff7faaff7fffff9f00ff9f55ff9faaff9fffffbf00ffbf55ffbfaaffbfffffdf00ffdf55ffdfaaffdfffffff55ffffaaccccffffccff33ffff66ffff99ffffccffff007f00007f55007faa007fff009f00009f55009faa009fff00bf0000bf5500bfaa00bfff00df0000df5500dfaa00dfff00ff5500ffaa2a00002a00552a00aa2a00ff2a1f002a1f552a1faa2a1fff2a3f002a3f55fffbf0a0a0a4808080ff000000ff00ffff000000ffff00ff00ffffffffff";

// The native crest: 16x16 palette indices, one byte each.
const CREST_SIZE = 16;
const CREST_BYTES = 256; // CREST_SIZE * CREST_SIZE

// The client's "no crest" revision mark beside zero.
const CREST_NONE = 0xffffffff;

// Overhead offsets from the guild label's left edge (client 86B350).
const GUILD_CREST_LEFT = -20;
const UNION_CREST_LEFT = -36;

/*
================
decodeGuildCrest

Expand the 256 palette indices to opaque RGBA, flipping the bottom-up
rows exactly once.
================
*/
export function decodeGuildCrest( bytes: Uint8Array ): Uint8ClampedArray<ArrayBuffer> {
	if ( bytes.length !== CREST_BYTES ) throw Error( "Invalid native crest length" );
	const palette = Uint8Array.from( paletteHex.match( /../g )!, v => parseInt( v, 16 ) ),
		pixels = new Uint8ClampedArray( CREST_BYTES * 4 );
	for ( let y = 0; y < CREST_SIZE; y++ ) {
		for ( let x = 0; x < CREST_SIZE; x++ ) {
			const c = bytes[(CREST_SIZE - 1 - y) * CREST_SIZE + x]! * 3, o = (y * CREST_SIZE + x) * 4;
			pixels[o] = palette[c]!;
			pixels[o + 1] = palette[c + 1]!;
			pixels[o + 2] = palette[c + 2]!;
			pixels[o + 3] = 255;
		}
	}
	return pixels;
}

/*
================
guildCrestFiles

The native file names a character's guild and union crests stand under,
with each one's overhead offset; a zero or no-change revision draws none.
================
*/
export function guildCrestFiles(
	prefix: number,
	guildId: number,
	crests: readonly [number, number, number]
): readonly { file: string; left: number; }[] {
	const out: { file: string; left: number; }[] = [];
	if ( guildId && crests[0] > 0 && crests[0] !== CREST_NONE ) {
		out.push( { file: `G${prefix >>> 0}_${guildId >>> 0}_${crests[0] >>> 0}.crb`, left: GUILD_CREST_LEFT } );
	}
	if ( crests[1] > 0 && crests[2] > 0 && crests[2] !== CREST_NONE ) {
		out.push( { file: `A${prefix >>> 0}_${crests[1] >>> 0}_${crests[2] >>> 0}.crb`, left: UNION_CREST_LEFT } );
	}
	return out;
}

/*
================
guildCrestUrl

Where a crest file is fetched: the Agent's /marks/ route under the API
base. The base keeps its path (hosted and dev edges route only /api to
Agent), so the route is appended rather than resolved from the root.
================
*/
export function guildCrestUrl( marksBase: string, file: string ): string {
	return marksBase.replace( /\/+$/, "" ) + "/marks/" + file;
}

/*
================
entityCrestFiles

The crest files over one entity. Name-keyed replacement notices affect
every resident member of that guild; the local character and members of
the local union read the union crest from the social state.
================
*/
export function entityCrestFiles( entity: EntityState, game: GameplayState, prefix: number ) {
	const social = game.social,
		own = entity.gid === game.localGid,
		name = own ? social?.guild?.name : entity.guildName;
	const patch = social?.crestUpdates?.find( r => r.name === name ),
		id = patch?.guildId ?? (own ? social?.guild?.id : entity.guildId);
	const allied = social?.alliances?.some( a => a.id === id );
	const source = own ?
		[ social?.guild?.crest ?? 0, ...(social?.allianceCrests ?? [ 0, 0 ]) ] :
		allied ?
		[ entity.guildCrests?.[0] ?? 0, ...(social?.allianceCrests ?? [ 0, 0 ]) ] :
		entity.guildCrests;
	if ( !id ) return [];
	return guildCrestFiles( prefix, id, [
		patch?.crest ?? source?.[0] ?? 0,
		patch?.allianceId ?? source?.[1] ?? 0,
		patch?.allianceCrest ?? source?.[2] ?? 0
	] );
}
