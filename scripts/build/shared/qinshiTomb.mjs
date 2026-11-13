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

import { serverSourceRoot } from "../world/paths.mjs";
import { loadServerOverlayRows, shardSwitchEnabled, shardSwitchPosture } from "./shardSwitch.mjs";

const QINSHI_TOMB_ENV = "SRO_QINSHI_TOMB";

// Mirrors monster.QinShiTombFromEnv: 1, on, true or yes.
const QINSHI_TOMB_ON_WORDS = [ "1", "on", "true", "yes" ];

// A degree-10 compound branch: heavy_10_aa.bsr, spear_10.bsr.
const DEGREE_10_BRANCH = /_10(?=(?:_[a-z]+)?\.bsr$)/i;

/*
================
qinshiTombShardPosture

SRO_QINSHI_TOMB as the deployment sets it (shardSwitch.mjs).
================
*/
export function qinshiTombShardPosture() {
	return shardSwitchPosture( QINSHI_TOMB_ENV );
}

/*
================
qinshiTombEnabled

An explicit SRO_QINSHI_TOMB wins (any non-blank value, "off" included);
unset or blank takes the shard's deployed posture.
================
*/
export function qinshiTombEnabled() {
	return shardSwitchEnabled( QINSHI_TOMB_ENV, QINSHI_TOMB_ON_WORDS );
}

/*
================
qinshiTombCombatEnabled

Port-only: arena encounters need cave actors and skills, never cave doors.
================
*/
export function qinshiTombCombatEnabled() {
	const arena = (process.env.SRO_SOOK_SURVIVAL_ARENA ?? "").trim().toLowerCase();
	return qinshiTombEnabled() || [ "1", "true", "on" ].includes( arena );
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
	if ( !qinshiTombCombatEnabled() || !DEGREE_10_BRANCH.test( branchPath ) ) {
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
loadQinShiTombCharacterRows

The 79 Qin-Shi characterdata rows (61 MOB_TQ_* and 18 MOB_QT_TQ_*,
remapped into the v1.150 ID gaps 14791..14851 and 22638..22655).
================
*/
export function loadQinShiTombCharacterRows() {
	return loadServerOverlayRows( "internal/game/world/monster/data/qinshi_tomb_characterdata.tsv" );
}

/*
================
loadQinShiTombSkillRows

The 230 Qin-Shi skilldata rows (MSKILL_*_TQ_*, remapped into unoccupied
v1.150 gap IDs).
================
*/
export function loadQinShiTombSkillRows() {
	return loadServerOverlayRows( "internal/game/enterworld/data/qinshi_tomb_skilldata.tsv" );
}

/*
================
loadQinShiTombTeleportLinkRows

The 77 Qin-Shi teleportlink rows (72 walk-in links + 5 clickable building
gate links 125 and 131..134).
================
*/
export function loadQinShiTombTeleportLinkRows() {
	return loadServerOverlayRows( "internal/game/action/data/qinshi_tomb_teleportlink.tsv" );
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
