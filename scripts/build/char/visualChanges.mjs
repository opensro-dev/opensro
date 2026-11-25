/*
===========================================================================

visualChanges.mjs - the costume a lent Spirit skill puts on its caster

Port-only, not native to v1.150 (SRO_NASRUN, #4407). The later clients'
charactervisualchange.txt dresses a character while a condition holds;
ConditionID 3 is a skill's buff, its codename in StrPram1. The Nasrun
skill import stages the Devil's Spirit's lent skills' rows alone
(nasrun_import.py VISUAL_CHANGE_TABLE): one row per costume piece - the
hat, the shoulders and the body - each with its slot mask, priority and
male/female BSR. The v1.150 tree has no such table, so a native build
publishes none.

Each piece is published as an avatar record of the equipment catalog
under a reserved id above every RefItemID (VISUAL_CHANGE_ID_BASE), so the
appearance assembly dresses it as it dresses any avatar: its mask hides
what it covers, its priority admits it before the worn equipment
(equipment-visuals.ts selectEquipmentVisuals). dress.visualChanges names
each skill's pieces by those ids.

===========================================================================
*/
import path from "node:path";
import { existsSync } from "node:fs";
import { listTextDataShardNamesSync, readTextDataRowsSync } from "../shared/textDataIo.mjs";

export const VISUAL_CHANGE_TABLE = "charactervisualchange.txt";
// charactervisualchange.txt columns (the later client's header).
const VC_SERVICE = 0;
const VC_CONDITION = 3;
const VC_SKILL = 9;
const VC_SLOT_MASK = 13;
const VC_PRIORITY = 16;
const VC_ANIMATION = 18;
const VC_BSR_M = 19;
const VC_BSR_F = 20;
// ConditionID 3: the row holds while the StrPram1 skill's buff does.
const CONDITION_SKILL_BUFF = 3;
// skilldata columns: the id and the codename.
const SKILL_ID = 1;
const SKILL_CODENAME = 3;
// The avatar sockets a piece is dressed in, by the slot mask's lead bits:
// the hat (0x0800 helm), the shoulders (0x0004) or the body.
const AVATAR_SLOT_HAT = 0;
const AVATAR_SLOT_DRESS = 1;
const AVATAR_SLOT_ATTACHMENT = 2;
const MASK_HAT = 0x0800;
const MASK_SHOULDER_ONLY = 0x0204;
// A skill holds at most this many pieces; ids step by it.
const PIECES_PER_SKILL = 4;
// The reserved id band: far above every RefItemID (the trees' stay under
// a million), and still a decimal catalog key the client admits.
export const VISUAL_CHANGE_ID_BASE = 2_000_000_000;

/*
================
modelPath

The equipment catalog's model path for a table BSR.
================
*/
function modelPath( raw ) {
	return raw.trim().replaceAll( "\\", "/" ).toLowerCase();
}

/*
================
pieceAvatarSlot
================
*/
function pieceAvatarSlot( mask ) {
	if ( mask & MASK_HAT ) return AVATAR_SLOT_HAT;
	if ( mask === MASK_SHOULDER_ONLY ) return AVATAR_SLOT_ATTACHMENT;
	return AVATAR_SLOT_DRESS;
}

/*
================
loadVisualChanges

The tree's skill-buff costume pieces: [{ id, skillId, skill, record,
models: { M, W } }], record the avatar catalog fields but its bodies.
Empty without the table.
================
*/
export function loadVisualChanges( root ) {
	const table = path.join( root, VISUAL_CHANGE_TABLE );
	if ( !existsSync( table ) ) return [];
	const skillIds = new Map();
	for ( const file of listTextDataShardNamesSync( root, /^skilldata_.*\.txt$/i ) ) {
		for ( const c of readTextDataRowsSync( path.join( root, file ) ) ) {
			if ( c[0] === "1" && c[SKILL_CODENAME] ) skillIds.set( c[SKILL_CODENAME].trim(), Number( c[SKILL_ID] ) );
		}
	}
	const pieces = [], perSkill = new Map();
	for ( const c of readTextDataRowsSync( table ) ) {
		if ( c[VC_SERVICE] !== "1" || Number( c[VC_CONDITION] ) !== CONDITION_SKILL_BUFF ) continue;
		const skill = c[VC_SKILL].trim(), skillId = skillIds.get( skill );
		if ( !skillId ) throw Error( `Visual change names unknown skill ${skill}` );
		const index = perSkill.get( skillId ) ?? 0;
		if ( index >= PIECES_PER_SKILL ) throw Error( `Visual change ${skill} has over ${PIECES_PER_SKILL} pieces` );
		perSkill.set( skillId, index + 1 );
		const mask = Number( c[VC_SLOT_MASK] );
		pieces.push( {
			id: VISUAL_CHANGE_ID_BASE + skillId * PIECES_PER_SKILL + index,
			skillId,
			skill,
			animation: c[VC_ANIMATION].trim(),
			record: {
				code: `VISUAL_CHANGE_${skill}_${index}`,
				slot: null,
				armorClass: 0,
				thiefSuit: false,
				visualMask: mask,
				visualPriority: Number( c[VC_PRIORITY] ),
				source: null,
				avatarSlot: pieceAvatarSlot( mask )
			},
			models: { M: modelPath( c[VC_BSR_M] ), W: modelPath( c[VC_BSR_F] ) }
		} );
	}
	return pieces;
}
