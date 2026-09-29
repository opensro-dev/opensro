/*
===========================================================================

resolveCharRoster.mjs - character codenames to model resources and movement stats

Resolves codenames to their model (.bsr) and walk/run speeds exactly the
way the native client does.

RE (revtool):
The title crowd (SpawnEuropIntroCrowd 0x4E26D0) picks a codename from a 60-entry
roster, then resolves it through the CharacterData (RefObjChar) table to a refObjId
and its AssocFileObj128 model path. CICharactor_ApplyMovementMode(bionic, 2) = walk (0x858450)
selects the RefObjChar walk-speed stat for locomotion.

Ground truth = server_dep/silkroad/textdata/characterdata*.txt (UTF-16LE, tab-separated).
Each RefObjChar row, around the model path, lists: Walk Run Berserk <...> <modelPath.bsr>.
e.g. CHAR_EU_MAN_ADVENTURER -> "...16 50 100 0 4 0 char\europe\europeman_adventurer.bsr"
so Walk=col(bsr)-6, Run=col(bsr)-5, Berserk=col(bsr)-4. Verified across EU/CH/COS rows.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { isMainScript } from "../shared/fsUtils.mjs";
import { listTextDataShardNamesSync, readTextDataLinesSync, splitTextDataRow } from "../shared/textDataIo.mjs";

const CODENAME_COL = 2;

/**
 * The native title crowd roster (60 codenames) spawned by
 * SpawnEuropIntroCrowd (0x4E26D0 @0x4e2745+). COS_T_* entries are rideable mounts.
 */
export const TITLE_CROWD_ROSTER = [
	"CHAR_EU_MAN_ADVENTURER",
	"CHAR_EU_MAN_ANGEL",
	"CHAR_EU_MAN_BARBARIAN",
	"CHAR_EU_MAN_DEVIL",
	"CHAR_EU_MAN_EXORCIST",
	"CHAR_EU_MAN_GLADIATOR",
	"CHAR_EU_MAN_KNIGHT",
	"CHAR_EU_MAN_MERCHANT",
	"CHAR_EU_MAN_NECROMENCER",
	"CHAR_EU_MAN_NOBLE",
	"CHAR_EU_MAN_PRIEST",
	"CHAR_EU_MAN_WARRIOR",
	"CHAR_EU_MAN_WEREWOLF",
	"CHAR_EU_WOMAN_ADVENTURER",
	"CHAR_EU_WOMAN_AMAZONESS",
	"CHAR_EU_WOMAN_ANGEL",
	"CHAR_EU_WOMAN_CRUSADER",
	"CHAR_EU_WOMAN_DEVIL",
	"CHAR_EU_WOMAN_GLADIATOR",
	"CHAR_EU_WOMAN_KNIGHT",
	"CHAR_EU_WOMAN_MERCHANT",
	"CHAR_EU_WOMAN_NOBLE",
	"CHAR_EU_WOMAN_ORACLE",
	"CHAR_EU_WOMAN_SUCCUBUS",
	"CHAR_EU_WOMAN_SUMMONER",
	"CHAR_EU_WOMAN_WITCH",
	"CHAR_CH_MAN_ADVENTURER",
	"CHAR_CH_MAN_BOGY",
	"CHAR_CH_MAN_FIGHTER",
	"CHAR_CH_MAN_MERCHANT",
	"CHAR_CH_MAN_MONK",
	"CHAR_CH_MAN_MONKEY",
	"CHAR_CH_MAN_NECROMANCER",
	"CHAR_CH_MAN_NOBLEBOY",
	"CHAR_CH_MAN_PERFORMER",
	"CHAR_CH_MAN_PRIEST",
	"CHAR_CH_MAN_SCHOLAR",
	"CHAR_CH_MAN_TATTOO",
	"CHAR_CH_MAN_WARRIOR",
	"CHAR_CH_WOMAN_ADVENTURER",
	"CHAR_CH_WOMAN_ASSASSIN",
	"CHAR_CH_WOMAN_BOGY",
	"CHAR_CH_WOMAN_FIGHTER",
	"CHAR_CH_WOMAN_FOX",
	"CHAR_CH_WOMAN_KANGSI",
	"CHAR_CH_WOMAN_KISAENG",
	"CHAR_CH_WOMAN_MERCHANT",
	"CHAR_CH_WOMAN_NECROMENCERB",
	"CHAR_CH_WOMAN_NECROMENCERW",
	"CHAR_CH_WOMAN_NOBLEGIRL",
	"CHAR_CH_WOMAN_SCHOLAR",
	"CHAR_CH_WOMAN_WARRIOR",
	"COS_T_DONKEY",
	"COS_T_HORSE1",
	"COS_T_HORSE2",
	"COS_T_HORSE3",
	"COS_T_CAMEL1",
	"COS_T_CAMEL2",
	"COS_T_CAMEL3",
	"COS_T_DHORSE1"
];

/*
================
loadCharacterDataRows

Load one explicitly selected characterdata identity family. RefObj codenames
are primary keys, so duplicate rows fail instead of inheriting filesystem
enumeration order through Map's otherwise-silent last-write-wins behavior.
================
*/
export function loadCharacterDataRows(
	textdataDir,
	{ codenamePattern = /^(CHAR_|COS_T_)/ } = {}
) {
	const files = listTextDataShardNamesSync( textdataDir, /^characterdata.*\.txt$/i );
	const rows = new Map();
	const sources = new Map();
	for ( const file of files ) {
		const lines = readTextDataLinesSync( path.join( textdataDir, file ) );
		for ( const [lineIndex, line] of lines.entries() ) {
			const cols = splitTextDataRow( line );
			const codename = cols[CODENAME_COL];
			codenamePattern.lastIndex = 0;
			if ( codename && codenamePattern.test( codename ) ) {
				const source = `${file}:${lineIndex + 1}`;
				const priorSource = sources.get( codename );
				if ( priorSource ) {
					throw new Error(
						`Duplicate characterdata codename ${codename}: ${priorSource} and ${source}`
					);
				}
				rows.set( codename, cols );
				sources.set( codename, source );
			}
		}
	}
	return rows;
}

/*
================
resolveCharModel

Resolve a single codename to { codename, refObjId, bsrPath, region, gender, isMount,
walkSpeed, runSpeed, berserkSpeed }. Returns null if the row or model path is missing.
================
*/
export function resolveCharModel( codename, rows ) {
	const cols = rows.get( codename );
	if ( !cols ) return null;
	const bsrIndex = cols.findIndex( ( v ) => /\.bsr$/i.test( v ) );
	if ( bsrIndex < 6 ) return null;
	const bsrPath = cols[bsrIndex].replaceAll( "\\", "/" ).toLowerCase();
	const region = bsrPath.match( /(?:^|\/)char\/([^/]+)\// )?.[1] ??
		(bsrPath.startsWith( "cos/" ) ? "cos" : "unknown");
	const isMount = /^COS_T_/.test( codename );
	return {
		codename,
		refObjId: Number( cols[1] ),
		bsrPath: `res/${bsrPath}`,
		region,
		isMount,
		walkSpeed: Number( cols[bsrIndex - 6] ),
		runSpeed: Number( cols[bsrIndex - 5] ),
		berserkSpeed: Number( cols[bsrIndex - 4] )
	};
}

/** Resolve the whole roster (or a provided codename list). */
/*
================
resolveRoster
================
*/
export function resolveRoster( textdataDir, codenames = TITLE_CROWD_ROSTER ) {
	const rows = loadCharacterDataRows( textdataDir );
	const resolved = [];
	const missing = [];
	for ( const codename of codenames ) {
		const model = resolveCharModel( codename, rows );
		if ( model ) resolved.push( model );
		else missing.push( codename );
	}
	return { resolved, missing };
}

// ---- CLI: print the resolved roster as a table ----
if ( isMainScript( import.meta.url ) ) {
	const { retailTextdataRoot } = await import( "../world/paths.mjs" );
	const textdataDir = process.argv[2] ?? retailTextdataRoot;
	const { resolved, missing } = resolveRoster( textdataDir );
	for ( const m of resolved ) {
		console.log(
			`${m.codename.padEnd( 28 )} ${String( m.refObjId ).padStart( 6 )}  ${m.region.padEnd( 6 )} ` +
				`walk=${m.walkSpeed} run=${m.runSpeed}${m.isMount ? " [mount]" : ""}  ${m.bsrPath}`
		);
	}
	console.log(
		`\nresolved ${resolved.length}/${resolved.length + missing.length}` +
			(missing.length ? `  MISSING: ${missing.join( ", " )}` : "")
	);
}
