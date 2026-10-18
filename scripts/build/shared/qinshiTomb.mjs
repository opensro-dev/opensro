/*
===========================================================================

qinshiTomb.mjs - the Qin-Shi Emperor Tomb textdata overlays for the bake

The v1.150 textdata has no Qin-Shi Emperor Tomb rows: the 79 monster
characterdata rows and 230 skilldata rows live only in the server's
committed overlays (apps/server, from #1154), because the server itself
appends them behind SRO_QINSHI_TOMB (monster.WithQinShiTomb, port-only,
not native; off by default). The client bake reads the same two files
through here so a flag-on bake resolves the Tomb references exactly the
way the flag-on server spawns them, and a flag-off bake never sees them.

===========================================================================
*/

import fs from "node:fs";
import path from "node:path";
import { splitTextDataRow } from "./textDataIo.mjs";
import { rebuildRoot } from "../world/paths.mjs";

/*
================
qinshiTombEnabled

Mirrors monster.QinShiTombFromEnv: SRO_QINSHI_TOMB trimmed and folded to
lower case must be 1, on, true or yes.
================
*/
export function qinshiTombEnabled() {
	const value = (process.env.SRO_QINSHI_TOMB ?? "").trim().toLowerCase();
	return value === "1" || value === "on" || value === "true" || value === "yes";
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
	return path.resolve( process.env.SRO_SERVER_SOURCE_ROOT ?? path.join( rebuildRoot, "apps", "server" ) );
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
