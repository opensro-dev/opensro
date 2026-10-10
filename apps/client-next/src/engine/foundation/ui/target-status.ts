/*
===========================================================================

target-status.ts - authored target selection and compact presentation

Native selection keeps its original branches and immutable authored nodes.
The owner-approved compact projection reflows those nodes without changing
health, difficulty, race, job or grade meaning, or scaling text fonts.

===========================================================================
*/
import type { EntityState } from "@/engine/contracts/world";
import type { UiRect } from "@/engine/contracts/ui";
import type { AuthoredControl, AuthoredLayout } from "./authored-layout";
const root = "/assets/images/Media_extracted/interface/";
/*
================
monsterGrade
================
*/
function monsterGrade( grade: number ) {
	const grades: Readonly<Record<number, readonly [string, string, number]>> = {
		0: [ "normal", "UIIT_CTL_WARENETWORK_DETAIL_NORMAL", 1 ],
		1: [ "champion", "UIIT_STT_MOB_CHAMPION", 2 ],
		3: [ "unique", "UIIT_STT_MOB_UNIQUE", 1 ],
		4: [ "giant", "UIIT_STT_MOB_GIANT", 20 ],
		5: [ "titan", "UIIT_STT_MOB_TITAN", 100 ],
		6: [ "elite", "UIIT_STT_MOB_ELITE", 4 ]
	};
	return grades[grade];
}
/*
================
targetDifficulty
================
*/
export function targetDifficulty( targetLevel: number, localLevel: number ) {
	const d = targetLevel - localLevel;
	return d <= -7 ? 0 : d <= -4 ? 1 : d <= 0 ? 2 : d <= 5 ? 3 : 4;
}
/*
================
monsterMaximumHp
================
*/
export function monsterMaximumHp( entity: EntityState ) {
	const factor = monsterGrade( entity.rarity ?? 0 )?.[2];
	return entity.maxHp === undefined || factor === undefined ?
		undefined :
		entity.maxHp * factor * (entity.rarityAuxIcon === 1 ? 10 : 1);
}
// 5814d0 selects one child, centers its actual width and uses y=7. Child
// layout mutations are values; authored resources remain immutable.
// The TypeID word: TID1 in bits 2..4, TID2 in 5..6, TID3 in 7..10 and TID4
// from bit 11, so the low mask holds TID1..TID3.
const TID_LOW_MASK = 0x7fc;
const TID_1_2_4 = 0x244;
const TID_1_2_5 = 0x2c4;
// iftw_fortressstructure.txt's window after 516BC0 resizes it: the root
// 236x51 (the frame's authored UVs span 236 texels), HP 195x4, name 177x12.
const FORTRESS_TARGET_WIDTH = 236;
const FORTRESS_TARGET_GAUGE_WIDTH = 195;
const FORTRESS_TARGET_NAME_WIDTH = 177;

/*
================
fortressTargetKind

CIFTargetStatusPanel_UpdateContent (5814D0) gives these targets the
fortress-structure window (layout kind +0x384) instead of the creature one:
a guard NPC (1/2/4/4 or 1/2/4/1) kind 1, a barricade structure (1/2/5/6)
kind 2, a fortress-war object (1/2/4/3) kind 3. Zero for every other target.
================
*/
export function fortressTargetKind( entity: Pick<EntityState, "kind" | "tidWord"> ): number {
	const tid = entity.tidWord ?? 0, low = tid & TID_LOW_MASK, tid4 = tid >>> 11;
	if ( entity.kind === "structure" ) {
		if ( low === TID_1_2_5 && tid4 === 6 ) return 2;
		if ( low === TID_1_2_4 && tid4 === 3 ) return 3;
	} else if ( entity.kind === "npc" && low === TID_1_2_4 ) {
		if ( tid4 === 4 || tid4 === 1 ) return 1;
		if ( tid4 === 3 ) return 3;
	}
	return 0;
}

/*
================
targetStatus
================
*/
export function targetStatus(
	layouts: Readonly<Record<string, AuthoredLayout>>,
	entity: EntityState,
	localLevel: number,
	hp: number | undefined,
	copy: ( key: string ) => string,
	measure: ( value: string ) => number,
	previousGradeIcon: string
) {
	const images: { node: AuthoredControl; fraction?: number; }[] = [],
		texts: { node: AuthoredControl; value: string; }[] = [];
	const roots = layouts.iftargetwindow!;
	let width = 196, height = 51, frame = roots.GDR_TW_COMMONENEMY!, gradeIcon = previousGradeIcon;
	/*
 ================
 change
 ================
 */
	const change = ( node: AuthoredControl, fields: Partial<AuthoredControl> ) => ({ ...node, ...fields });
	/*
 ================
 texture
 ================
 */
	const texture = ( node: AuthoredControl, path: string ) =>
		change( node, { texture: root + path + ".png", uv: [ 0, 0, 1, 1 ] } );
	/*
 ================
 put
 ================
 */
	const put = ( node: AuthoredControl, value: string ) => texts.push( { node, value } );
	if ( entity.kind === "monster" ) {
		height = 78;
		frame = roots.GDR_TW_SPECIALMOBWND!;
		const p = layouts.iftw_specialmob!,
			grade = monsterGrade( entity.rarity ?? 0 ),
			difficulty = targetDifficulty( entity.level ?? localLevel, localLevel );
		const gems = [ "weak2", "weak1", "normal", "strong1", "strong2" ],
			colors = [ 0x87d2ff, 0xa5e0ce, 0xffffff, 0xffb387, 0xff8787 ],
			color = colors[difficulty]!;
		images.push( { node: texture( p.GDR_TWSM_GEM!, "targetwindow/tw_gem_" + gems[difficulty] ) } );
		put(
			change( p.GDR_TWSM_TEXT_ID!, {
				color: [ (color >>> 16) / 255, ((color >>> 8) & 255) / 255, (color & 255) / 255, 1 ]
			} ),
			entity.name
		);
		put( p.GDR_TWSM_LEVEL!, "Lv " + (entity.level ?? 0) );
		if ( grade ) gradeIcon = root + "targetwindow/tw_icon_" + grade[0] + ".png";
		let title = grade ? copy( grade[1] ) : "";
		if ( entity.rarityAuxIcon === 1 ) title += copy( "UIIT_STT_PARTYMOB_TARGET" );
		let icon = p.GDR_TWSM_ICON!, titleNode = p.GDR_TWSM_TEXT_LEV!;
		if ( entity.rarityAuxIcon === 1 && (entity.rarity ?? 0) !== 0 ) {
			const x = Math.trunc( (196 - measure( title )) / 2 ) + (gradeIcon ? 10 : 0);
			titleNode = change( titleNode, { rect: [ x, 56, 168, 12 ] } );
			icon = change( icon, { rect: [ x - 20, 56, 16, 16 ] } );
		}
		if ( gradeIcon ) images.push( { node: change( icon, { texture: gradeIcon, uv: [ 0, 0, 1, 1 ] } ) } );
		put( titleNode, title );
		const max = monsterMaximumHp( entity );
		if ( hp !== undefined && max ) {
			images.push( { node: p.GDR_TWSM_GAUGE_HPGAUGE!, fraction: Math.max( 0, Math.min( 1, hp / max ) ) } );
		}
	} else if ( entity.kind === "player" ) {
		const job = entity.jobType ?? 4,
			working = job !== 4 && job !== 0,
			p = layouts[working ? "iftw_jobplayer_trijob2" : "iftw_player"]!;
		height = working ? 58 : 36;
		frame = roots[working ? "GDR_TW_JOB_PLAYERWND" : "GDR_TW_PLAYERWND"]!;
		const race = entity.countryByte9c, kindred = p[working ? "GDR_TWJP_KINDRED_MARK" : "GDR_TW_KINDRED_MARK"]!;
		if ( race === 0 || race === 1 ) {
			images.push( { node: texture( kindred, "ifcommon/com_kindred_" + (race === 0 ? "china" : "europe") ) } );
		}
		put( p[working ? "GDR_TWJP_JOB_ALIAS" : "GDR_TWP_TEXT_NAME"]!, entity.name );
		if ( working ) {
			const jobName = [ "", "merchant", "thief", "hunter" ][job];
			if ( jobName ) {
				images.push( { node: texture( p.GDR_TWJP_JOB_ICON!, "targetwindow/tw_job_" + jobName ) } );
				const grade = entity.jobGrade ?? 0;
				put(
					p.GDR_TWJP_JOB_GRADENAME!,
					copy( "UIIT_STT_CLASS_" + (race === 1 ? "EU_" : "") + jobName.toUpperCase() + "_" + grade )
				);
				put( p.GDR_TWJP_JOB_GRADE!, grade + copy( "UIIT_STT_GRADE" ) );
			}
		}
	} else if ( fortressTargetKind( entity ) ) {
		// 516BC0 resizes the fortress-structure child; 5814D0 gives a structure
		// the normal gem and a guard NPC the player gem.
		const p = layouts.iftw_fortressstructure!;
		width = FORTRESS_TARGET_WIDTH;
		frame = roots.GDR_TW_FORTRESSSTRUCTER!;
		images.push( {
			node: texture(
				p.GDR_TWFS_GEM!,
				"targetwindow/tw_gem_" + (entity.kind === "structure" ? "normal" : "player")
			)
		} );
		put( change( p.GDR_TWFS_TEXT_ID!, { rect: [ 34, 10, FORTRESS_TARGET_NAME_WIDTH, 12 ] } ), entity.name );
		if ( hp !== undefined && entity.maxHp ) {
			images.push( {
				node: change( p.GDR_TWFS_GAUGE_HPGAUGE!, { rect: [ 14, 37, FORTRESS_TARGET_GAUGE_WIDTH, 4 ] } ),
				fraction: Math.max( 0, Math.min( 1, hp / entity.maxHp ) )
			} );
		}
	} else if ( entity.kind === "npc" || entity.kind === "cos" || entity.kind === "structure" ) {
		// A structure (CICATStruct) is a CICNPC: 5823B0 gives it the wide NPC
		// frame, and 5814D0 the normal gem.
		const p = layouts.iftw_commonenemy!,
			flags = entity.tidWord ?? 0,
			compact = entity.kind === "cos" && (flags & 0x7fe) === 0x1c6 && [ 3, 4, 5 ].includes( flags >>> 11 );
		let nameWidth = 137, gaugeWidth = 168;
		if ( entity.kind === "npc" || entity.kind === "structure" ) {
			width = 236;
			nameWidth = 177;
			gaugeWidth = 208;
			frame = change( frame, {
				uv: [ .723632991, .220703006, .95410198 - .723632991, .320313007 - .220703006 ]
			} );
		} else if ( compact ) {
			height = 36;
			nameWidth = 122;
			frame = change( frame, { uv: [ .53027302, .644531012, .721678972 - .53027302, .714842975 - .644531012 ] } );
		}
		images.push( {
			node: texture(
				p.GDR_TWCE_GEM!,
				"targetwindow/tw_gem_" +
					(entity.kind === "cos" ? "animal" : entity.kind === "structure" ? "normal" : "player")
			)
		} );
		put( change( p.GDR_TWCE_TEXT_ID!, { rect: [ 34, 10, nameWidth, 12 ] } ), entity.name );
		if ( !compact && hp !== undefined && entity.maxHp ) {
			images.push( {
				node: change( p.GDR_TWCE_GAUGE_HPGAUGE!, { rect: [ 14, 37, gaugeWidth, 4 ] } ),
				fraction: Math.max( 0, Math.min( 1, hp / entity.maxHp ) )
			} );
		}
	} else return null;
	images.unshift( { node: change( frame, { rect: [ 0, 0, width, height ] } ) } );
	const close = change( roots.GDR_TW_CLOSE!, {
		rect: [ width - 20, 9, 16, 16 ],
		texture: root + "ifcommon/com_windowclose.png",
		uv: [ 0, 0, 1, 1 ]
	} );
	return { width, height, images, texts, close, gradeIcon };
}

const COMPACT_TARGET_WIDTH = 196;
const COMPACT_TARGET_MIN_WIDTH = 80;
const COMPACT_METADATA_WIDTH = 120;
const COMPACT_TARGET_HEIGHT = 54;
const COMPACT_NARROW_MONSTER_HEIGHT = 68;
const COMPACT_NARROW_JOB_HEIGHT = 62;
const COMPACT_NAME_HEIGHT = 30;
const COMPACT_GAUGE_HEIGHT = 36;

/*
================
compactTargetStatus

Port-only, owner-approved mobile composition. Consume native output instead
of selecting another target family. Text values and font indices survive;
the consumer uses ellipsis and exposes helpText on the target hit region.
Below 120 pixels metadata takes two rows, preserving every native field and icon.
Below 80 pixels there is no usable name/close row: return null rather than
overflow the caller's reserved width or silently scale the whole panel.
================
*/
export function compactTargetStatus(
	output: NonNullable<ReturnType<typeof targetStatus>>,
	kind: EntityState["kind"],
	availableWidth: number
) {
	if ( !Number.isFinite( availableWidth ) || availableWidth < COMPACT_TARGET_MIN_WIDTH ) return null;
	const width = Math.min( COMPACT_TARGET_WIDTH, Math.floor( availableWidth ) );
	const monster = kind === "monster";
	const job = output.texts.some( row => row.node.name === "GDR_TWJP_JOB_ALIAS" );
	const narrow = width < COMPACT_METADATA_WIDTH;
	const gauge = output.images.some( image => image.fraction !== undefined );
	const height = monster ?
		(narrow ? COMPACT_NARROW_MONSTER_HEIGHT : COMPACT_TARGET_HEIGHT) :
		job ?
		(narrow ? COMPACT_NARROW_JOB_HEIGHT : COMPACT_TARGET_HEIGHT) :
		gauge ?
		COMPACT_GAUGE_HEIGHT :
		COMPACT_NAME_HEIGHT;
	const helpText = output.texts.map( row => row.value ).filter( Boolean ).join( "\n" );
	/*
	================
	place
	Authored client insets belong to the old frame, not the new text boxes.
	================
	*/
	function place( node: AuthoredControl, rect: UiRect ): AuthoredControl {
		return { ...node, rect, client: [ 0, 0, 0, 0 ] };
	}
	const images = output.images.map( ( image, index ) => {
		let rect: UiRect;
		if ( index === 0 ) rect = [ 0, 0, width, height ];
		else if ( image.fraction !== undefined ) rect = [ 8, 27, width - 16, 4 ];
		else if ( image.node.name === "GDR_TWSM_ICON" || image.node.name === "GDR_TWJP_JOB_ICON" ) {
			rect = monster ? [ 50, 35, 12, 12 ] : [ 8, narrow ? 29 : 35, 12, 12 ];
		} else rect = [ 4, 6, 16, 16 ];
		return { ...image, node: place( image.node, rect ) };
	} );
	const texts = output.texts.map( ( row, index ) => {
		let rect: UiRect;
		if ( index === 0 ) rect = [ 24, 7, width - 48, 14 ];
		else if ( monster ) {
			rect = row.node.name === "GDR_TWSM_LEVEL" ?
				[ 8, 35, 40, 12 ] :
				narrow ?
				[ 8, 51, width - 16, 12 ] :
				[ 65, 35, width - 70, 12 ];
		} else if ( narrow ) {
			rect = row.node.name === "GDR_TWJP_JOB_GRADENAME" ?
				[ 24, 29, width - 28, 12 ] :
				[ 8, 45, width - 16, 12 ];
		} else {
			// Job grade name and numeric grade remain separate native fields.
			rect = row.node.name === "GDR_TWJP_JOB_GRADENAME" ?
				[ 24, 35, width - 72, 12 ] :
				[ width - 44, 35, 40, 12 ];
		}
		return { ...row, node: place( row.node, rect ) };
	} );
	return {
		...output,
		width,
		height,
		images,
		texts,
		close: place( output.close, [ width - 20, 7, 16, 16 ] ),
		helpText
	};
}
