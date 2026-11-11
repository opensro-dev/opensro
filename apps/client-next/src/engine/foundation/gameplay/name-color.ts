import type { EntityState } from "@/engine/contracts/world";
import type { SocialState } from "./social";
import { fortressActive, fortressStatus, type FortressState } from "./fortress";

// ARGB values written by 858810/8567F0, retained by the entity owner.
export const NAME_COLOR_WHITE = 0xffffffff;
export const NAME_COLOR_GM = 0xffffd87a;
export const NAME_COLOR_HOSTILE = 0xffff6262;
export const NAME_COLOR_ASSAULT = 0xffff58fe;
export const NAME_COLOR_PARTY = 0xff62f5b1;
export const NAME_COLOR_COS = 0xffffff55;
export const NAME_COLOR_ENEMY_GUILD = 0xffff0000;
export const NAME_COLOR_NEUTRAL_WAR = 0xff1abaff;
export interface NameColorContext {
	readonly local: EntityState;
	readonly social: SocialState;
	readonly fortress: FortressState;
	readonly localItem?: { readonly typeFlags: number; readonly refObjId: number; };
	readonly capeTeam: ( refObjId: number ) => number | undefined;
	readonly attackedName?: string;
}
export function isNameColorGuard( e: Pick<EntityState, "kind" | "tidWord"> ): boolean {
	return e.kind === "npc" && ((e.tidWord ?? 0) & 0x7fc) === 0x244;
}
export function jobItemType( t: number ): number {
	return (t & 0x7fe) === 0x3ac ? t >>> 11 : 0;
}
export function equipmentHoldType( t: number | undefined ): number {
	const kind = t === undefined ? 0 : jobItemType( t );
	return kind >= 1 && kind <= 3 ? kind : 4;
}
export function entityHoldType( entity: EntityState ): number {
	return entity.holdType ?? equipmentHoldType( entity.equipment?.find( r => r.slot === 8 )?.typeFlags );
}
export function partyName( s: SocialState, name: string ): boolean {
	return s.leader !== 0 && s.members.some( r => r.name === name );
}
// The roster test in 858810 (PartyManager_ContainsMember, 8229B0) reads the
// character's one name field (+0x108). Entering in job mode rewrites the
// local player's field with the job alias (8675F0 via 8645A0) and parks the
// real name in a side field (864BB0), so a suited local player matches the
// roster's alias row and turns party green like any member. The port splits
// that one field into name (real) and boardName (alias), so the roster test
// reads the board name when one is set (#3214).
export function colourProbeName( e: Pick<EntityState, "name" | "boardName"> ): string {
	return e.boardName ?? e.name;
}
function opposite( a: number, b: number ) {
	return (a === 1 || a === 3) && b === 2 || a === 2 && (b === 1 || b === 3);
}
function guildId( e: EntityState, c: NameColorContext ) {
	return e.kind === "local-player" ? (c.social.guild?.id ?? 0) : (e.guildId ?? 0);
}
function fortressColor( e: EntityState, c: NameColorContext ): number {
	const local = c.social.guild?.id ?? 0, target = guildId( e, c );
	if ( !local || !target ) return NAME_COLOR_WHITE;
	const status = fortressStatus( c.fortress, local, target, c.social.alliances?.map( r => r.id ) ?? [] );
	if ( c.fortress.registered.includes( local ) ) return status === 0xcc ? NAME_COLOR_HOSTILE : NAME_COLOR_WHITE;
	return status === 0xca ? NAME_COLOR_HOSTILE : status === 0xcc ? NAME_COLOR_NEUTRAL_WAR : NAME_COLOR_WHITE;
}
// 858810 returns without a write for some non-user states; preserve old color.
export function refreshNameColor( e: EntityState, c: NameColorContext ): number {
	const user = e.kind === "player" || e.kind === "local-player",
		own = e.kind === "local-player",
		pvp = e.pvpState ?? 0;
	const party = partyName( c.social, colourProbeName( e ) ),
		war = fortressActive( c.fortress ),
		hold = entityHoldType( e );
	const enemy = ( name: string | undefined ) => name !== undefined && !!c.social.wars?.some( r => r.name === name );
	if ( user ) {
		if ( e.name.startsWith( "[GM]" ) ) return NAME_COLOR_GM;
		const team = c.local.arenaTeam ?? 0xff;
		if ( team !== 0xff ) return team === (e.arenaTeam ?? 0xff) ? NAME_COLOR_WHITE : NAME_COLOR_HOSTILE;
		if ( war && guildId( e, c ) ) return party ? NAME_COLOR_PARTY : fortressColor( e, c );
		const targetItem = e.equipment?.find( r => r.slot === 8 ),
			localType = c.localItem ? jobItemType( c.localItem.typeFlags ) : 0,
			targetType = targetItem ? jobItemType( targetItem.typeFlags ) : 0;
		const cape = targetType === 5 && localType === 5, job = hold !== 4 && opposite( hold, localType );
		if ( !own && (enemy( e.guildName ) || cape || job) ) {
			const base = e.name === c.attackedName || opposite( hold, entityHoldType( c.local ) ) || pvp === 1 ||
				pvp === 2;
			const targetTeam = targetItem ? c.capeTeam( targetItem.refObjId ) : undefined,
				localTeam = c.localItem ? c.capeTeam( c.localItem.refObjId ) : undefined;
			if ( cape && (targetTeam === undefined || localTeam === undefined) ) {
				throw Error( "Missing native PvP cape team parameter" );
			}
			if ( base || enemy( e.guildName ) || cape && (targetTeam !== localTeam || targetTeam === 5) ) {
				return NAME_COLOR_HOSTILE;
			}
		}
	}
	if ( isNameColorGuard( e ) && war && guildId( e, c ) ) return fortressColor( e, c );
	if ( e.kind === "cos" ) return NAME_COLOR_COS;
	if ( pvp === 1 ) return NAME_COLOR_ASSAULT;
	if ( pvp === 2 ) return NAME_COLOR_HOSTILE;
	if ( user ) {
		if ( !own && hold !== 4 && guildId( e, c ) && enemy( e.guildName ) ) return NAME_COLOR_ENEMY_GUILD;
		return party ? NAME_COLOR_PARTY : NAME_COLOR_WHITE;
	}
	return pvp === 0 ? NAME_COLOR_WHITE : e.nameColor ?? NAME_COLOR_WHITE;
}
export function nameColorRgba( argb: number ): [number, number, number, number] {
	return [ (argb >>> 16 & 255) / 255, (argb >>> 8 & 255) / 255, (argb & 255) / 255, (argb >>> 24) / 255 ];
}
