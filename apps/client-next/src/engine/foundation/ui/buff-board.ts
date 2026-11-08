/*
===========================================================================

buff-board.ts - the buff and abnormal-state board (6E2030)

Lays out the timed buff cells, abnormal-state icons, COS timer bars and
param-job rows in the native eight-column grid, with the 1 s refresh's
suppressed slots left out.

===========================================================================
*/
import { countJobFraction, countJobHelpText, countJobRemainingMs } from "@/engine/foundation/gameplay/count-job";
import { buffDepartureFrame } from "@/engine/foundation/gameplay/buff-slots";
import { abnormalBarFraction } from "@/engine/foundation/gameplay/abnormal-snapshot";
import type { GameplayState, PetTraining } from "@/engine/contracts/gameplay";
import type { UiRect } from "@/engine/contracts/ui";
import { petOwnerStacks, petSkillIcon, petSkillIconUV } from "./pet-skill-icons";
import { PET_DESIGN } from "./pet-design-data";
import { effectRemainingMs } from "@/engine/foundation/gameplay/attached-effects";
import { cosTimerBars } from "@/engine/foundation/gameplay/cos-timer";
import { paramJobFraction, paramJobHelpText, paramJobRemainingMs } from "@/engine/foundation/gameplay/param-job";
import { iconPath } from "./icon";
import { skillMetadataById } from "@/engine/foundation/gameplay/skill-catalog";
import { mostWantedRow } from "@/engine/foundation/gameplay/most-wanted";
export const buffTimerRoot = "/assets/images/Media_extracted/icon/stateodd/";
// 6E6E00 kinds 7/8: the Academy buff bar's full scale is the saved
// buff's 24 h window (0x5265C00 ms), and the slot-7 pass is offered
// once an hour of the window has accumulated (6E2840).
export const TC_BUFF_WINDOW_MS = 0x5265c00;
const ACADEMY_BUFF_WINDOW_MINUTES = 1440;
const TC_BUFF_PASS_AFTER_MINUTES = 60;

/*
================
buffBoard

6E2030: local 20px cells, eight columns, 23x27 pitch. The secondary
group reserves at least one primary row and adds a two-pixel gap.
suppressed holds board slot serials flagged by the 1 s refresh (6E64A0).
================
*/
export function buffBoard(
	game: Pick<
		GameplayState,
		| "localGid"
		| "buffSlots"
		| "cosWindows"
		| "paramJobs"
		| "countJobs"
		| "academy"
		| "vitals"
		| "abnormalRecords"
		| "petTailwind"
		| "petGuidance"
		| "petGuidance"
		| "petTraining"
		| "mostWanted"
		| "skillCatalog"
		| "skillIndex"
	>,
	timeMs: number,
	suppressed: ReadonlySet<number> = new Set(),
	ownerPet?: PetTraining,
	showPetTraining = true,
	copy: ( key: string ) => string = key => key
) {
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
		const remaining = effectRemainingMs( effect, timeMs ),
			duration = effect.durationMs,
			isSuppressed = suppressed.has( slot.serial );
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
			helpSource: {
				kind: "effect" as const,
				gid: effect.gid,
				token: effect.token,
				skill: effect.skill,
				...(isSuppressed ? { suppressed: true } : {})
			},
			id: "buff:" + effect.token + ":" + effect.skill,
			path,
			label: skill?.name ?? "",
			suppressed: isSuppressed,
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
	// remaining seconds against the internal item's duration. The hover
	// help (6E2C90 kind 4) reads the item's name, its total duration and
	// the remained period; paramJobHelpText composes it from the text keys.
	for ( const job of game.paramJobs ?? [] ) {
		const path = iconPath( job.reference.icon );
		if ( path ) {
			const left = Math.max(
				0,
				Math.ceil( (job.remainingSec * 1000 - Math.max( 0, timeMs - job.receivedAtMs )) / 1000 )
			);
			primary.push( {
				helpText: job.reference.description ?
					`${job.reference.name}\n${job.reference.description}\n${left}s remaining` :
					paramJobHelpText(
						job.reference.name ?? "",
						job.reference.durationSec,
						paramJobRemainingMs( job, timeMs ),
						copy
					),
				id: "param-job:" + job.itemRefObjId,
				path,
				label: job.reference.name ?? "",
				fraction: paramJobFraction( job, job.reference, timeMs )
			} );
		}
	}
	// 6E6E00 kind 5: a premium package's limited uses, one slot per package
	// with its icon and its period's bar. The hover help (6E2C90) reads
	// the package's name, the remained period and one line per limited
	// row; countJobHelpText composes it from the client's text keys.
	const packages = new Map<number, NonNullable<typeof game.countJobs>[number][]>();
	for ( const row of game.countJobs ?? [] ) {
		packages.set( row.packageRefObjId, [ ...(packages.get( row.packageRefObjId ) ?? []), row ] );
	}
	for ( const [packageRefObjId, rows] of packages ) {
		const reference = rows[0]!.reference, path = iconPath( reference.icon );
		if ( path ) {
			primary.push( {
				helpText: countJobHelpText( reference.name ?? "", rows, countJobRemainingMs( rows[0]!, timeMs ), copy ),
				id: "count-job:" + packageRefObjId,
				path,
				label: reference.name ?? "",
				fraction: countJobFraction( rows[0]!, reference.periodSec, timeMs )
			} );
		}
	}
	// 6E6E00 kinds 7/8: the Academy cumulated buffs - the saved row
	// (slot 7) and one row per received buff (slot 8), each counted
	// down from its receipt against the 24 h window.
	const academy = game.academy;
	if ( academy?.savedBuff ) {
		const remaining = tcBuffRemainingMs( academy.savedBuff, timeMs );
		if ( remaining > 0 ) {
			primary.push( {
				helpText: tcBuffHelpText(
					"UIIT_CTL_TC_SAVE_BUFF",
					"UIIT_CTL_TC_SAVE_BUFF_POINT",
					remaining,
					academy.savedBuff.value,
					undefined,
					copy
				),
				id: "academy-buff-saved",
				path: iconPath( "skill\\etc\\tc_reserve_buff.ddj" ) ?? "",
				label: copy( "UIIT_CTL_TC_SAVE_BUFF" ),
				fraction: Math.min( 1, remaining / TC_BUFF_WINDOW_MS ),
				...(ACADEMY_BUFF_WINDOW_MINUTES - academy.savedBuff.minutes >= TC_BUFF_PASS_AFTER_MINUTES ?
					{ passBuff: true } :
					{})
			} );
		}
	}
	for ( const row of academy?.receivedBuffs ?? [] ) {
		const remaining = tcBuffRemainingMs( row, timeMs );
		if ( remaining <= 0 ) continue;
		primary.push( {
			helpText: tcBuffHelpText(
				"UIIT_CTL_TC_RESERVE_BUFF",
				"UIIT_CTL_TC_RESERVE_BUFF_POINT",
				remaining,
				row.value,
				academy?.members?.find( m => m.id === row.memberId )?.name,
				copy
			),
			id: "academy-buff-received:" + row.memberId,
			path: iconPath( "skill\\etc\\tc_amends_buff.ddj" ) ?? "",
			label: copy( "UIIT_CTL_TC_RESERVE_BUFF" ),
			fraction: Math.min( 1, remaining / TC_BUFF_WINDOW_MS )
		} );
	}
	for ( const row of petOwnerStacks( ownerPet ) ) {
		primary.push( {
			id: "owner-pet-stack:" + row.design.id,
			path: petSkillIcon( row.design ),
			uv: petSkillIconUV( row.design ),
			label: row.label,
			helpText: row.helpText,
			stack: row.count,
			ready: row.ready,
			fraction: Math.min( 1, row.count / row.maximum )
		} );
	}
	const guidance = game.petGuidance;
	if ( showPetTraining && guidance && guidance.gid === game.localGid ) {
		const design = PET_DESIGN.treeSkills.find( row => row.id === guidance.book );
		if ( design ) {
			primary.push( {
				id: "pet-guidance",
				path: petSkillIcon( design ),
				uv: petSkillIconUV( design ),
				label: `Precision: ${guidance.stacks}/5${
					guidance.stacks === 5 && guidance.book >= 59 ? " - Skill ready" : ""
				}`,
				helpText:
					`Guiding Feathers\nHit ratio +${guidance.hitPercent}%\n${guidance.stacks}/5 stacks\nCrow hits refresh 10 s duration.\n` +
					(guidance.book >= 59 ?
						"At 5 stacks, your next successful damaging skill is empowered and consumes Precision." :
						""),
				stack: guidance.stacks,
				ready: guidance.stacks === 5 && guidance.book >= 59,
				fraction: guidance.stacks / 5
			} );
		}
	}
	const tailwind = game.petTailwind;
	if ( showPetTraining && tailwind && tailwind.gid === game.localGid ) {
		const design = PET_DESIGN.treeSkills.find( row => row.id === tailwind.book );
		if ( design ) {
			primary.push( {
				id: "pet-tailwind",
				path: petSkillIcon( design ),
				uv: petSkillIconUV( design ),
				label: `Tailwind: ${tailwind.stacks}/10${
					tailwind.stacks === 10 && tailwind.book >= 56 ? " â€” Stun ready" : ""
				}`,
				helpText:
					`Tailwind\nMovement speed +${tailwind.speedPercent}%\n${tailwind.stacks} / 10 stacks\nOne stack every 5 s without dealing or taking damage.\n` +
					(tailwind.book >= 56 ?
						"At 10 stacks: your next hit dealt or taken stuns that enemy for 2 s.\n" :
						"") +
					"Damage removes all stacks.",
				stack: tailwind.stacks,
				ready: tailwind.stacks === 10 && tailwind.book >= 56,
				fraction: tailwind.stacks / 10
			} );
		}
	}
	// Port-only (#1849): the streak holder's own wanted row, shown
	// whenever the shard sends the 0x3FBA state (owner 2026-11-06,
	// #2721: the server's SRO_MOST_WANTED flag is the only switch, with
	// no client opt-in; no state leaves the native board). It wears the
	// dressed job's icon (the row waits while the suit is off, as its
	// bonuses do).
	const wantedRow = mostWantedRow( game.mostWanted, timeMs );
	if ( wantedRow ) {
		primary.push( {
			id: "most-wanted",
			path: wantedRow.path,
			label: wantedRow.label,
			helpText: wantedRow.helpText,
			fraction: wantedRow.fraction
		} );
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
/*
================
Entry
================
*/
interface Entry {
	suppressed?: boolean;
	cancel?: { mode: "direct" | "confirm"; skillId: number; token: number; instance: number; };
	// The saved Academy buff past its first hour: clicking the slot
	// asks the pass question (6E2840) instead of cancelling a buff.
	passBuff?: boolean;
	departure?: { frame: number; alpha: number; };
	helpText?: string;
	helpSource?: import("@/engine/contracts/ui").UiHelpSource;
	id: string;
	path: string;
	uv?: UiRect;
	stack?: number;
	ready?: boolean;
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

/*
================
tcBuffRemainingMs

770E70 / 770970 hold a show state's minutes from its receipt
instant; the slot counts minutes*60000+1 ms down from there.
================
*/
function tcBuffRemainingMs( buff: { minutes: number; receivedAtMs: number; }, timeMs: number ): number {
	return Math.max( 0, buff.minutes * 60000 + 1 - (timeMs - buff.receivedAtMs) );
}

/*
================
tcBuffHelpText

6E2C90 kinds 7/8: the title, a blank line, the remain-time line and
the point line; a received row names its sender in between.
================
*/
function tcBuffHelpText(
	titleKey: string,
	pointKey: string,
	remainingMs: number,
	value: number,
	sender: string | undefined,
	copy: ( key: string ) => string
): string {
	const minutes = Math.trunc( remainingMs / 60000 );
	const lines = [
		copy( titleKey ),
		" ",
		`${copy( "UIIT_STT_REMAIN_TIME" )}: ${Math.trunc( minutes / 60 )}${copy( "PARAM_HOUR" )} ${minutes % 60}${
			copy( "PARAM_MINUTE" )
		}`
	];
	if ( sender !== undefined ) lines.push( `${copy( "UIIT_STT_LETTER_SENDER" )}: ${sender}` );
	lines.push( `${copy( "UIIT_STT_SILKMALL_P_POINT" )}: ${value} ${copy( pointKey )}` );
	return lines.join( "\n" );
}
