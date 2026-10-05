/*
===========================================================================

guild-manager.ts - the guild manager NPC's talk rows and level prices

A guild manager's select word carries 0x4000 (service 0xF). The menu
builder (CIFNPCTalk_BuildMenuFromCapabilities 5D9100) then lists, outside
a guild, the create row (0x12); inside one, the master's level-up (0x17),
disband (0x14), master leave (0x18) and war compensation (0x1B) rows or a
member's leave row (0x1C); then the release row (0x19) while no release
vote runs, else the vote row (0x1A); then the warehouse row (0x1D). The
level-up window (CIFGuildLevelUp 5EF9A0) prices the next level from the
GP (0xBE4C58) and gold (0xBE4C6C) tables by the current level.

===========================================================================
*/
import type { Guild } from "./social";

// The guild set's capability bit.
export const GUILD_CAPABILITY = 0x4000;
// The master release vote's kind (SGuildData_FindVoteEntry(0), 82CC00).
export const MASTER_RELEASE_VOTE = 0;
// The last level the window offers (5EF9A0 clamps the next level at 5).
export const GUILD_MAX_LEVEL = 5;

export type GuildManagerRow =
	| "create"
	| "level-up"
	| "dissolve"
	| "master-leave"
	| "compensation"
	| "secede"
	| "release"
	| "vote"
	| "warehouse";

/*
================
rowLabel
================
*/
function rowLabel( row: GuildManagerRow ): string {
	switch ( row ) {
		case "create":
			return "UIIT_CTL_GUILD_CREATE";
		case "level-up":
			return "UIIT_MSG_GUILD_LEVEL_UP";
		case "dissolve":
			return "UIIT_CTL_GUILD_BREAK";
		case "master-leave":
			return "UIIT_CTL_MASTERLEAVE";
		case "compensation":
			return "UIIT_CTL_GUILD_COMPENSATION";
		case "secede":
			return "UIIT_CTL_GUILD_SECESSION";
		case "release":
			return "UIIT_CTL_MASTERRELEASE";
		case "vote":
			return "UIIT_CTL_MRELEASE_VOTE";
		case "warehouse":
			return "UIIT_CTL_GUILD_WAREHOUSE";
	}
}

/*
================
guildManagerRows

5D9100's guild set for the local player, as row ids and label symbols.
================
*/
export function guildManagerRows(
	capabilities: number,
	guild: Guild | null | undefined,
	localName: string
): readonly { readonly row: GuildManagerRow; readonly symbol: string; }[] {
	if ( !(capabilities & GUILD_CAPABILITY) ) return [];
	const rows: GuildManagerRow[] = [];
	if ( !guild ) {
		rows.push( "create" );
	} else {
		const master = guild.members.some( m => m.grade === 0 && m.name === localName );
		rows.push(
			...(master ? [ "level-up", "dissolve", "master-leave", "compensation" ] as const : [ "secede" ] as const)
		);
		rows.push( guild.votes?.some( v => v.kind === MASTER_RELEASE_VOTE ) ? "vote" : "release" );
		rows.push( "warehouse" );
	}
	return rows.map( row => ({ row, symbol: rowLabel( row ) }) );
}

/*
================
guildLevelUpPrice

The GP and gold the next level costs, or undefined at the last level.
================
*/
export function guildLevelUpPrice( level: number ): { readonly gp: number; readonly gold: number; } | undefined {
	switch ( level ) {
		case 1:
			return { gp: 5400, gold: 3000000 };
		case 2:
			return { gp: 50400, gold: 9000000 };
		case 3:
			return { gp: 135000, gold: 15000000 };
		case 4:
			return { gp: 378000, gold: 21000000 };
	}
	return undefined;
}
