/*
===========================================================================

qinshiTomb.mjs - the Qin-Shi Emperor Tomb textdata overlays for the bake

The v1.150 textdata has no Qin-Shi Emperor Tomb rows: the 79 monster
characterdata rows and 230 skilldata rows live only in the server's
committed overlays (apps/server, from #1154), because the server itself
appends them behind SRO_QINSHI_TOMB (monster.WithQinShiTomb, port-only,
not native). The client bake reads the same two files through here so a
bake resolves the Tomb references exactly when the shard spawns them.

The bake follows the shard's deployed posture, not a bare build env:
an unset SRO_QINSHI_TOMB means what the deployment means by it - the
release profile's value, else the shard registry's default (on). The
bake once read unset as off while the shard defaulted on, so the live
shard spawned 79 species no shipped pack could draw (#2024).

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { splitTextDataRow } from "./textDataIo.mjs";
import { serverSourceRoot } from "../world/paths.mjs";

// The switch, its deployed profile, and the shard settings registry
// that holds its default (shardsettings.go, the server's one owner).
const QINSHI_TOMB_ENV = "SRO_QINSHI_TOMB";

// A degree-10 compound branch: heavy_10_aa.bsr, spear_10.bsr.
const DEGREE_10_BRANCH = /_10(?=(?:_[a-z]+)?\.bsr$)/i;
const SHARD_PROFILE = path.join( "ops", "release", "shard-settings.json" );
const SHARD_REGISTRY = path.join( "internal", "config", "shardsettings", "shardsettings.go" );

/*
================
flagWordOn

Mirrors monster.QinShiTombFromEnv: the value trimmed and folded to
lower case must be 1, on, true or yes.
================
*/
function flagWordOn( value ) {
	const word = String( value ).trim().toLowerCase();
	return word === "1" || word === "on" || word === "true" || word === "yes";
}

/*
================
qinshiTombShardPosture

The switch's value as the deployment sets it when the build env does
not: the release profile's entry, else the registry Default. A registry
row this cannot read throws - a posture the bake cannot see must stop
the build, never read as off.
================
*/
export function qinshiTombShardPosture() {
	const serverRoot = qinshiTombServerRoot();
	const profile = JSON.parse( fs.readFileSync( path.join( serverRoot, SHARD_PROFILE ), "utf8" ) );
	if ( typeof profile[QINSHI_TOMB_ENV] === "string" ) {
		return profile[QINSHI_TOMB_ENV];
	}
	const registry = fs.readFileSync( path.join( serverRoot, SHARD_REGISTRY ), "utf8" );
	const row = new RegExp( `\\{\\s*Env:\\s*"${QINSHI_TOMB_ENV}"[^}]*?Default:\\s*"([^"]*)"` ).exec( registry );
	if ( !row ) {
		throw new Error(
			`${QINSHI_TOMB_ENV}: no Default in ${SHARD_REGISTRY}; the bake cannot see the shard's posture`
		);
	}
	return row[1];
}

/*
================
qinshiTombEnabled

An explicit SRO_QINSHI_TOMB wins (any non-blank value, "off" included);
unset or blank takes the shard's deployed posture.
================
*/
export function qinshiTombEnabled() {
	const value = process.env[QINSHI_TOMB_ENV] ?? "";
	if ( value.trim() !== "" ) {
		return flagWordOn( value );
	}
	return flagWordOn( qinshiTombShardPosture() );
}

/*
================
qinshiTombBranchFallback

The degree-9 stand-in for a compound branch v1.150 does not ship, or
null. Port-only, not native: shinmu.cpd (the tomb flamemaster) names
degree-10 armor and spear BSRs that Data.pk2 lacks because v1.150 caps at
degree 9; with the tomb on, the matching _09 BSR from the same directory
stands in so the Floor 5 boss can assemble. The bake and the effect
references both take it, so they see the same branches.
================
*/
export function qinshiTombBranchFallback( branchPath ) {
	if ( !qinshiTombEnabled() || !DEGREE_10_BRANCH.test( branchPath ) ) {
		return null;
	}
	return branchPath.replace( DEGREE_10_BRANCH, "_09" );
}

/*
================
qinshiTombServerRoot

The server tree holding the overlays: SRO_SERVER_SOURCE_ROOT when set
(the same override the roster export uses), else this checkout's
apps/server.
================
*/
export function qinshiTombServerRoot() {
	return serverSourceRoot();
}

/*
================
loadQinShiTombRows

One overlay file as column rows. The files are UTF-8, tab-separated, in
the exact v1.150 characterdata/skilldata column layout, with `#` header
comment lines and blank lines skipped (the server's SplitTabbedText
framing).
================
*/
function loadQinShiTombRows( relativePath ) {
	const sourcePath = path.join( qinshiTombServerRoot(), relativePath );
	const text = fs.readFileSync( sourcePath, "utf8" ).replace( /^\ufeff/, "" );
	const rows = [];
	for ( const line of text.split( /\r?\n/ ) ) {
		if ( !line.trim() ) continue;
		const cols = splitTextDataRow( line );
		const first = String( cols[0] ?? "" ).trim();
		if ( first.startsWith( "#" ) || first.startsWith( "//" ) ) continue;
		rows.push( cols );
	}
	return { sourcePath, rows };
}

/*
================
loadQinShiTombCharacterRows

The 79 Qin-Shi characterdata rows (61 MOB_TQ_* and 18 MOB_QT_TQ_*,
remapped into the v1.150 ID gaps 14791..14851 and 22638..22655).
================
*/
export function loadQinShiTombCharacterRows() {
	return loadQinShiTombRows( "internal/game/world/monster/data/qinshi_tomb_characterdata.tsv" );
}

/*
================
loadQinShiTombSkillRows

The 230 Qin-Shi skilldata rows (MSKILL_*_TQ_*, remapped into unoccupied
v1.150 gap IDs).
================
*/
export function loadQinShiTombSkillRows() {
	return loadQinShiTombRows( "internal/game/enterworld/data/qinshi_tomb_skilldata.tsv" );
}

/*
================
loadQinShiTombTeleportLinkRows

The 77 Qin-Shi teleportlink rows (72 walk-in links + 5 clickable building
gate links 125 and 131..134).
================
*/
export function loadQinShiTombTeleportLinkRows() {
	return loadQinShiTombRows( "internal/game/action/data/qinshi_tomb_teleportlink.tsv" );
}

/*
================
isQinShiTombSkillEffectName

Port-only, not native: retail v1.150 skilleffect.txt ships the Qin-Shi
Emperor Tomb skillaniset2 rows (MSKILL_TQ_*, MSKILL_QT_TQ_*, the Tomb
thief quest skills MSKILL_QT_MOB_THIEF_ZOHAB_1 / SALSU_1, and the Tomb
EFFECT_AM_* ambient rows) with Service == 0 while their matching
#section skilleffectset rows are already present. Match that exact family
so parseSkillAniSet admits them only when SRO_QINSHI_TOMB is on.
================
*/
export function isQinShiTombSkillEffectName( codename ) {
	return /^(?:MSKILL_(?:QT_)?TQ_|MSKILL_QT_MOB_THIEF_(?:ZOHAB|SALSU)_|EFFECT_AM_(?:MOB_(?:QT_)?TQ_|NPC_CH_QT_FLAMEMASTER))/i
		.test( String( codename ?? "" ).trim() );
}
