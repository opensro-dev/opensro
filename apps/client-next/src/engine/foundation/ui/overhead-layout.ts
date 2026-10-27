/*
===========================================================================

overhead-layout.ts - the rows stacked over a character's name

The guild or companion-owner row, the vitals (quick status), the fortress
mark and crest, and a stall's title; speech rises above the highest row.
withoutBoardPass is the same stack with the /id byte clear: only the stall
title and speech board remain.

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
import { NAME_COLOR_WHITE, nameColorRgba } from "@/engine/foundation/gameplay/name-color";

// CICCos 855CBC subtracts the double 20.0 at C46198 before drawing the owner.
const COMPANION_OWNER_Y = -20;
// 854B95..854C02's tid-word gates: low byte 0xC6 and band 5 (server's
// MercenaryBand): every guild soldier reference carries this one word.
const GUILD_SOLDIER_TID_WORD = 0x29c6;
// The stall title (the +0x464 board) rides this far above the highest row.
const STALL_TITLE_RISE = 20;

/*
================
companionOwnerShown

CICCos 855C40 draws the owner board, and lifts the anchor the status gauge
then uses, only with a non-empty owner name (+78C) and the Options
"Monster names" byte (+E) set (855C51..855C67).
================
*/
function companionOwnerShown( entity: EntityState, options: GameOptions ): boolean {
	return entity.kind === "cos" && !!entity.ownerName && options.monsterNames;
}

/*
================
companionOwnerNameplate

854B54 copies the visible owner's name color into the COS owner board
(+794); with no visible owner, 854C0E paints it white. The spawn's owner
name stays either way.
================
*/
export function companionOwnerNameplate(
	entity: EntityState,
	owner: EntityState | undefined,
	options: GameOptions,
	ownerGuildColour?: UiQuad["color"]
) {
	if ( !companionOwnerShown( entity, options ) ) return null;
	const user = owner?.gid === entity.ownerGid && (owner?.kind === "player" || owner?.kind === "local-player");
	// 854B95..854C21: a guild soldier's owner board deviates. With the
	// owner visible it takes the owner guild label colour (the guild row
	// colour above, computed by the caller for the owner entity), and
	// 854C0C skips the white reset when the owner is absent, so it keeps
	// its own name colour instead of painting white.
	if ( entity.tidWord === GUILD_SOLDIER_TID_WORD ) {
		return {
			text: entity.ownerName ?? "",
			color: user && ownerGuildColour ?
				ownerGuildColour :
				user ?
				nameColorRgba( owner.nameColor ?? NAME_COLOR_WHITE ) :
				nameColorRgba( entity.nameColor ?? NAME_COLOR_WHITE ),
			y: COMPANION_OWNER_Y
		};
	}
	return {
		text: entity.ownerName ?? "",
		color: nameColorRgba( user ? owner.nameColor ?? NAME_COLOR_WHITE : NAME_COLOR_WHITE ),
		y: COMPANION_OWNER_Y
	};
}

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
		ownerY = companionOwnerShown( entity, options ) ? COMPANION_OWNER_Y : 0,
		statusY = Math.min( guildY, ownerY ) - (status ? 25 : 0);
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
		stallY = statusY - (stallText ? STALL_TITLE_RISE : 0);
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

export type OverheadLayout = ReturnType<typeof overheadLayout>;

/*
================
withoutBoardPass

The overhead stack while the /id byte (0xCCB5D8) is clear. 85F5DC branches
past both the name board and the kind-4 overhead pass (vtable+0x3C at
85F5FD), so no guild row, fortress mark or crest, companion-owner row or
quick-status gauge draws, and none of them lifts the board anchor (+0x4E4;
86B350, 855C40 and 85F660 each subtract their row). The +0x464 board (stall
title or speech) is past the branch at 85F609 and still draws, from the
unlifted anchor.
================
*/
export function withoutBoardPass( layout: OverheadLayout ): OverheadLayout {
	const stallY = layout.stallText ? -STALL_TITLE_RISE : 0;
	return {
		...layout,
		fortressCrest: null,
		guildText: "",
		guildY: 0,
		status: null,
		statusY: 0,
		fortressMark: null,
		speechY: stallY,
		stallY
	};
}
