/*
===========================================================================

overhead-layout.ts - the rows stacked over a character's name

The guild row, the vitals (quick status), the fortress mark and crest, and
a stall's title above them all; speech rises above the highest row.

===========================================================================
*/
import { iconPath } from "./icon";
import type { EntityState } from "@/engine/contracts/world";
import type { GameplayState } from "@/engine/contracts/gameplay";
import type { GameOptions } from "@/engine/foundation/gameplay/game-options";
import type { UiQuad } from "@/engine/contracts/ui";
import { quickStatus } from "./quick-status";
import { fortressActive, fortressStatus } from "@/engine/foundation/gameplay/fortress";
import { STALL_TITLE_MODE } from "@/engine/foundation/gameplay/interaction-approach";
/*
================
overheadLayout

85F4A0 dispatches kind 4 independently of the base-name gate. 86B350
consumes 20 pixels for the guild row, then 85F660 consumes 25 for vitals.
================
*/
export function overheadLayout(
	entity: EntityState,
	local: EntityState | undefined,
	game: GameplayState,
	options: GameOptions,
	copy: ( key: string ) => string = () => ""
) {
	const social = game.social, own = entity.gid === game.localGid;
	const guild = own ? social?.guild : undefined, name = own ? guild?.name : entity.guildName;
	const grant = own ? guild?.members.find( m => m.name === social?.localName )?.grant : entity.guildGrantName;
	const hold = entity.equipment?.find( i => i.slot === 8 )?.typeFlags;
	const plain = hold === undefined || ![ 0x800, 0x1000, 0x1800 ].includes( hold & 0xf800 );
	// +7E0 is a guild office bit, not a separate attacker/defender team.
	const role = social?.roleUpdates?.find( r => r.name === entity.name )?.role ??
		(own ? guild?.members.find( m => m.name === social?.localName )?.role : entity.guildWarTeam);
	const roleKey = ({
		1: "COMMANDER",
		2: "SUBCOMMANDER",
		4: "BATTLEMANAGER",
		8: "PRODUCTMANAGER",
		16: "TRAINERMANAGER",
		32: "ENGINEER"
	} as Record<number, string>)[role ?? 0];
	const roleText = roleKey && game.fortress &&
			(fortressActive( game.fortress ) || game.fortress.wars.some( w => w.name === name )) ?
		copy( "UIIT_STT_FORT_GUILD_" + roleKey ) :
		"";
	const guildText = options.guildNames && plain && name ?
		"[" + name + (grant ? " * " + grant : "") + (roleText ? (grant ? "" : " * ") + "(" + roleText + ")" : "") +
		"]" :
		"";
	const color: UiQuad["color"] = name && name === social?.guild?.name ?
		[ 254 / 255, 173 / 255, 46 / 255, 1 ] :
		social?.alliances?.some( g => g.name === name ) ?
		[ 160 / 255, 254 / 255, 104 / 255, 1 ] :
		social?.wars?.some( w => w.name === name ) ?
		[ 1, 0, 0, 1 ] :
		[ 102 / 255, 185 / 255, 1, 1 ];
	const status = quickStatus( entity, local, game, options ),
		guildY = guildText ? -20 : 0,
		statusY = guildY - (status ? 25 : 0);
	let fortressMark: { path: string; y: number; } | null = null;
	const war = game.fortress, team = role, targetGuild = own ? guild?.id : entity.guildId;
	if ( options.fortressNames && war && fortressActive( war ) && targetGuild && (team === 1 || team === 2) ) {
		const localGuild = social?.guild?.id ?? 0,
			verdict = fortressStatus( war, localGuild, targetGuild, social?.alliances?.map( a => a.id ) ?? [] );
		const kind = verdict === 0xc9 || verdict === 0xcb ?
			"fortress" :
			verdict === 0xca || verdict === 0xcc && war.registered.includes( localGuild ) ?
			"defensive" :
			verdict === 0xcc ?
			"aggressive" :
			null;
		if ( kind ) {
			fortressMark = {
				path: "/assets/images/Media_extracted/icon/etc/mark_" + kind + (team + 1) + ".png",
				y: -(options.guildNames ? 64 : 44) - (own ?
					options.ownStatus ? 16 : 0 :
					options.partyStatus && social?.members.some( m => m.name === entity.name ) ?
					16 :
					0)
			};
		}
	}
	const ownerFortress = game.fortress?.wars.find( w => w.name === name ),
		fortressCrest = ownerFortress ?
			iconPath( game.fortress?.fortresses.find( f => f.id === ownerFortress.id )?.icon ) :
			null;
	// 751430 / 86A26D: a stall's title rides above the rest (SetOverheadStallTitle, 0xFFFEB5FF).
	const stallText = entity.appearanceState?.[6] === STALL_TITLE_MODE && entity.titleText ? entity.titleText : "",
		stallY = statusY - (stallText ? 20 : 0);
	return {
		fortressCrest,
		guildText,
		guildColor: color,
		guildY,
		status,
		statusY,
		speechY: stallY,
		fortressMark,
		stallText,
		stallY
	};
}
