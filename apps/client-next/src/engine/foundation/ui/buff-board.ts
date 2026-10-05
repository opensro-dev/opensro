/*
===========================================================================

buff-board.ts - the buff and abnormal-state board (6E2030)

Lays out the timed buff cells, abnormal-state icons, COS timer bars and
param-job rows in the native eight-column grid, with the 1 s refresh's
suppressed slots left out.

===========================================================================
*/
import { countJobFraction } from "@/engine/foundation/gameplay/count-job";
import { buffDepartureFrame } from "@/engine/foundation/gameplay/buff-slots";
import { abnormalBarFraction } from "@/engine/foundation/gameplay/abnormal-snapshot";
import type { GameplayState } from "@/engine/contracts/gameplay";
import { effectRemainingMs } from "@/engine/foundation/gameplay/attached-effects";
import { cosTimerBars } from "@/engine/foundation/gameplay/cos-timer";
import { paramJobFraction } from "@/engine/foundation/gameplay/param-job";
import { iconPath } from "./icon";
import { skillMetadataById } from "@/engine/foundation/gameplay/skill-catalog";
export const buffTimerRoot = "/assets/images/Media_extracted/icon/stateodd/";

/*
================
buffBoard

6E2030: local 20px cells, eight columns, 23x27 pitch. The secondary
group reserves at least one primary row and adds a two-pixel gap.
suppressed holds board slot serials flagged by the 1 s refresh (6E64A0).
================
*/
export function buffBoard( game: GameplayState, timeMs: number, suppressed: ReadonlySet<number> = new Set() ) {
	const primary: Entry[] = [], secondary: Entry[] = [];
	for ( const slot of game.buffSlots ?? [] ) {
		const effect = slot.effect;
		if ( effect.gid !== game.localGid ) continue;
		const skill = skillMetadataById( game, effect.skill ), path = iconPath( skill?.icon );
		if ( !path ) continue;
		if ( slot.state === "departing" ) {
			const departure = buffDepartureFrame( slot, timeMs );
			if ( departure.frame < 9 ) {
				primary.push( { id: "buff-departing:" + slot.serial, path, label: "", fraction: null, departure } );
			}
			continue;
		}
		const remaining = effectRemainingMs( effect, timeMs ), duration = effect.durationMs;
		// 6E5D40 stores slot kind 1 for every 0xB419 effect, so 6E51A0 always takes
		// the single-bar branch of 6E7FA0. The alternate s_stateodd_time02_gauge slot
		// belongs to kind 3, which only 6E6E00 (opcode 0x3691) creates. This phase
		// byte is a 0xB419 payload field and does not select a native slot kind.
		const row = {
			cancel: !slot.secondary && skill?.buffCancel && skill.buffCancel !== "blocked" ?
				{
					mode: skill.buffCancel,
					skillId: effect.skill,
					token: skill.buffCancelInstance ? effect.token : 0,
					instance: effect.token
				} :
				undefined,
			helpSource: { kind: "effect" as const, gid: effect.gid, token: effect.token, skill: effect.skill },
			id: "buff:" + effect.token + ":" + effect.skill,
			path,
			label: skill?.name ?? "",
			suppressed: suppressed.has( slot.serial ),
			fraction: remaining !== null && duration !== undefined && duration > 0 ?
				Math.min( 1, remaining / duration ) :
				1
		};
		(slot.secondary ? secondary : primary).push( row );
	}
	// 6E6E00 kind 3 stores var_68 = 1, so an item window joins the primary list,
	// and 6E7FA0 gives it the two-bar slot. 6E6150 keys rows by item, so several
	// can run at once.
	for ( const window of game.cosWindows ?? [] ) {
		const bars = cosTimerBars( window, window.reference, timeMs ), path = iconPath( window.reference.icon );
		if ( bars && path ) {
			primary.push( {
				helpText: window.reference.name,
				id: "cos:" + window.itemRefObjId,
				path,
				label: window.reference.name ?? "",
				fraction: bars.primary,
				secondary: bars.secondary
			} );
		}
	}
	// 6E6E00 kind 4: an EXP/SP scroll job, one bar counted down from the
	// remaining seconds against the internal item's duration.
	for ( const job of game.paramJobs ?? [] ) {
		const path = iconPath( job.reference.icon );
		if ( path ) {
			primary.push( {
				helpText: job.reference.name,
				id: "param-job:" + job.itemRefObjId,
				path,
				label: job.reference.name ?? "",
				fraction: paramJobFraction( job, job.reference, timeMs )
			} );
		}
	}
	// 6E6E00 kind 5: a premium package's limited uses, one slot per package
	// with its icon and its period's bar; the help lists each item's uses.
	const packages = new Map<number, NonNullable<typeof game.countJobs>[number][]>();
	for ( const row of game.countJobs ?? [] ) {
		packages.set( row.packageRefObjId, [ ...(packages.get( row.packageRefObjId ) ?? []), row ] );
	}
	for ( const [packageRefObjId, rows] of packages ) {
		const reference = rows[0]!.reference, path = iconPath( reference.icon );
		if ( path ) {
			primary.push( {
				helpText: [ reference.name ?? "", ...rows.map( r => (r.itemName ?? "") + " x" + r.uses ) ].join( "\n" ),
				id: "count-job:" + packageRefObjId,
				path,
				label: reference.name ?? "",
				fraction: countJobFraction( rows[0]!, reference.periodSec, timeMs )
			} );
		}
	}
	for ( const row of abnormalBuffIcons( game.vitals?.find( v => v.gid === game.localGid )?.abnormal ?? 0 ) ) {
		const record = game.abnormalRecords?.find( item => item.bit === Number( row.id.slice( 9 ) ) );
		secondary.push( {
			...row,
			helpSource: { kind: "abnormal", gid: game.localGid, bit: Number( row.id.slice( 9 ) ) },
			fraction: record ? abnormalBarFraction( record, timeMs ) : null
		} );
	}
	return [
		...primary.map( ( row, i ) => ({ ...row, x: (i % 8) * 23, y: Math.floor( i / 8 ) * 27 }) ),
		...secondary.map( ( row, i ) => ({
			...row,
			x: (i % 8) * 23,
			y: (Math.max( 1, Math.ceil( primary.length / 8 ) ) + Math.floor( i / 8 )) * 27 + 2
		}) )
	];
}
interface Entry {
	suppressed?: boolean;
	cancel?: { mode: "direct" | "confirm"; skillId: number; token: number; instance: number; };
	departure?: { frame: number; alpha: number; };
	helpText?: string;
	helpSource?: import("@/engine/contracts/ui").UiHelpSource;
	id: string;
	path: string;
	label: string;
	fraction: number | null;
	secondary?: number | null;
}

/*
================
abnormalIcon

6E0010. Unknown bits reserve their native cell without inventing art.
================
*/
export function abnormalIcon( bit: number ) {
	const name = [
		"freeze",
		"frostbite",
		"electricshock",
		"burn",
		"poisoning",
		"zombi",
		"sleep",
		"root",
		"blunting",
		"fear",
		"myopia",
		"bleeding",
		"",
		"dark",
		"stun",
		"disease",
		"confusion",
		"decay",
		"weakness",
		"powerless",
		"dissociation",
		"panic",
		"combustion",
		"",
		"incubation"
	][bit];
	return { path: name ? buffTimerRoot + "s_" + name + "_icon.png" : "", label: name ?? "" };
}

/*
================
abnormalBuffIcons
================
*/
export function abnormalBuffIcons( mask: number ) {
	const rows: { id: string; path: string; label: string; }[] = [];
	for ( let bit = 0; bit < 32; bit++ ) {
		if ( mask & (2 ** bit) ) rows.push( { id: "abnormal:" + bit, ...abnormalIcon( bit ) } );
	}
	return rows;
}
